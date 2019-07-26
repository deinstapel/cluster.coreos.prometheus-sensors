package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/amkay/gosensors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var labelRegex = regexp.MustCompile("[\\w \\t-]+")

var (
	fanspeedDesc = prometheus.NewDesc(
		"sensor_lm_fan_speed_rpm",
		"fan speed (rotations per minute).",
		[]string{"fantype", "chip", "adaptor"},
		nil)

	voltageDesc = prometheus.NewDesc(
		"sensor_lm_voltage_volts",
		"voltage in volts",
		[]string{"intype", "chip", "adaptor"},
		nil)

	powerDesc = prometheus.NewDesc(
		"sensor_lm_power_watts",
		"power in watts",
		[]string{"powertype", "chip", "adaptor"},
		nil)

	temperatureDesc = prometheus.NewDesc(
		"sensor_lm_temperature_celsius",
		"temperature in celsius",
		[]string{"temptype", "chip", "adaptor"},
		nil)

	hddTempDesc = prometheus.NewDesc(
		"sensor_hddsmart_temperature_celsius",
		"temperature in celsius",
		[]string{"device", "id"},
		nil)
)

func main() {
	var (
		listenAddress = flag.String("web.listen-address", ":9255", "Address on which to expose metrics and web interface.")
		metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
	)
	flag.Parse()

	shutdownCtx, stopFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)

	// Required to keep hddtemp from crashing
	time.Sleep(1 * time.Second)

	startHddTemp(shutdownCtx, &wg)

	// Required to keep my own socket from running in ECONNREFUSED
	time.Sleep(1 * time.Second)

	// Register the HDD Temp collector
	hddcollector := NewHddCollector("localhost:7777")
	prometheus.MustRegister(hddcollector)

	// Register the LM Sensors collector
	lmscollector := NewLmSensorsCollector()
	lmscollector.Init()
	prometheus.MustRegister(lmscollector)

	// Remove the default process and golang collectors (those are not interesting anyway)
	prometheus.Unregister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{
		PidFn: func() (int, error) {
			return os.Getpid(), nil
		},
		Namespace: "",
	}))
	prometheus.Unregister(prometheus.NewGoCollector())

	go func() {
		defer wg.Done()
		// Multiplexer
		mux := http.NewServeMux()
		mux.Handle(*metricsPath, promhttp.Handler())
		mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`<html>
			<head><title>Sensor Exporter</title></head>
			<body>
			<h1>Sensor Exporter</h1>
			<p><a href="` + *metricsPath + `">Metrics</a></p>
			</body>
			</html>`))
		})
		srv := &http.Server{Addr: *listenAddress, Handler: mux}

		go func() {
			<-shutdownCtx.Done()
			srv.Close()
		}()

		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("HTTP Server failed: %v", err)
			os.Exit(2)
		}
		log.Printf("HTTP server exited clean")
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-stop
	log.Printf("Waiting for HTTP server and hddtemp to terminate")
	stopFunc()
	wg.Wait()
	log.Printf("Done, exiting")
}

func startHddTemp(ctx context.Context, wg *sync.WaitGroup) {
	files, err := ioutil.ReadDir("/dev")
	if err != nil {
		log.Printf("Failed to discover HDDs: %v", err)
		os.Exit(1)
	}
	r := regexp.MustCompile("^sd[a-z]+$")
	namesList := []string{}
	for _, f := range files {
		name := f.Name()
		if r.MatchString(name) {
			log.Printf("Discovered HDD: /dev/%v", name)
			namesList = append(namesList, fmt.Sprintf("/dev/%s", name))
		}
	}

	execPath, err := exec.LookPath("hddtemp")
	if err != nil {
		log.Printf("Failed to find hddtemp util: %v", err)
		os.Exit(2)
	}

	args := []string{}
	args = append(args, "-d", "-F", "-l", "127.0.0.1", "-p", "7777")
	args = append(args, namesList...)

	cmd := exec.Command(execPath, args...)
	log.Printf("Running %s", strings.Join(cmd.Args, " "))
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	doneChan := make(chan bool, 1)
	killed := false
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start hddtemp: %v", err)
	}
	go func() {
		if cmd.Wait(); !killed {
			log.Printf("hddtemp exited (not killed) with exit code: %v", cmd.ProcessState.ExitCode())
			doneChan <- true
		} else {
			log.Printf("hddtemp exited (killed) with exit code: %v", cmd.ProcessState.ExitCode())
			doneChan <- false
		}
	}()
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			killed = true
			cmd.Process.Signal(syscall.SIGINT)
		case killProcess := <-doneChan:
			if killProcess {
				os.Exit(1)
			}
		}
	}()
}

type LmSensorsCollector struct{}

func NewLmSensorsCollector() *LmSensorsCollector {
	return &LmSensorsCollector{}
}

func (l *LmSensorsCollector) Init() {
	gosensors.Init()
}

// Describe implements prometheus.Collector.
func (l *LmSensorsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- fanspeedDesc
	ch <- powerDesc
	ch <- temperatureDesc
	ch <- voltageDesc
}

// Collect implements prometheus.Collector.
func (l *LmSensorsCollector) Collect(ch chan<- prometheus.Metric) {
	for _, chip := range gosensors.GetDetectedChips() {
		chipName := chip.String()
		adaptorName := chip.AdapterName()
		for _, feature := range chip.GetFeatures() {
			if strings.HasPrefix(feature.Name, "fan") {
				ch <- prometheus.MustNewConstMetric(fanspeedDesc,
					prometheus.GaugeValue,
					feature.GetValue(),
					feature.GetLabel(), chipName, adaptorName)
			} else if strings.HasPrefix(feature.Name, "temp") {
				ch <- prometheus.MustNewConstMetric(temperatureDesc,
					prometheus.GaugeValue,
					feature.GetValue(),
					feature.GetLabel(), chipName, adaptorName)
			} else if strings.HasPrefix(feature.Name, "in") {
				ch <- prometheus.MustNewConstMetric(voltageDesc,
					prometheus.GaugeValue,
					feature.GetValue(),
					feature.GetLabel(), chipName, adaptorName)
			} else if strings.HasPrefix(feature.Name, "power") {
				ch <- prometheus.MustNewConstMetric(powerDesc,
					prometheus.GaugeValue,
					feature.GetValue(),
					feature.GetLabel(), chipName, adaptorName)
			}
		}
	}
}

type (
	HddCollector struct {
		address string
		conn    net.Conn
		buf     bytes.Buffer
	}

	HddTemperature struct {
		Device             string
		Id                 string
		TemperatureCelsius float64
	}
)

func NewHddCollector(address string) *HddCollector {
	return &HddCollector{
		address: address,
	}
}

func (h *HddCollector) Init() error {
	conn, err := net.Dial("tcp", h.address)
	if err != nil {
		return fmt.Errorf("error connecting to hddtemp address '%s': %v", h.address, err)
	}
	h.conn = conn
	return nil
}

func (h *HddCollector) readTempsFromConn() (string, error) {
        if err := h.Init(); err != nil {
		return "", err
	}

	h.buf.Reset()
	_, err := io.Copy(&h.buf, h.conn)
	if err != nil {
		return "", fmt.Errorf("Error reading from hddtemp socket: %v", err)
	}
	h.conn.Close()
	return h.buf.String(), nil
}

func (h *HddCollector) Close() error {
	if err := h.conn.Close(); err != nil {
		return fmt.Errorf("Error closing hddtemp socket: %v", err)
	}
	return nil
}

func parseHddTemps(s string) ([]HddTemperature, error) {
	var hddtemps []HddTemperature
	if len(s) < 1 || s[0] != '|' {
		return nil, fmt.Errorf("Error parsing output from hddtemp: %s", s)
	}
	for _, item := range strings.Split(s[1:len(s)-1], "||") {
		hddtemp, err := parseHddTemp(item)
		if err != nil {
			return nil, fmt.Errorf("Error parsing output from hddtemp: %v", err)
		}
		hddtemps = append(hddtemps, hddtemp)
	}
	return hddtemps, nil
}

func parseHddTemp(s string) (HddTemperature, error) {
	pieces := strings.Split(s, "|")
	if len(pieces) != 4 {
		return HddTemperature{}, fmt.Errorf("error parsing item from hddtemp, expected 4 tokens: %s", s)
	}
	dev, id, temp, unit := pieces[0], pieces[1], pieces[2], pieces[3]
	log.Printf("Got data set", dev, id, temp, unit, labelRegex.FindString(id))
	id = strings.TrimSpace(labelRegex.FindString(id))

	if unit == "*" {
		return HddTemperature{Device: dev, Id: id, TemperatureCelsius: -1}, nil
	}

	if unit != "C" {
		return HddTemperature{}, fmt.Errorf("error parsing item from hddtemp, I only speak Celsius: %s", s)
	}

	ftemp, err := strconv.ParseFloat(temp, 64)
	if err != nil {
		return HddTemperature{}, fmt.Errorf("Error parsing temperature as float: %s", temp)
	}

	return HddTemperature{Device: dev, Id: id, TemperatureCelsius: ftemp}, nil
}

// Describe implements prometheus.Collector.
func (e *HddCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- hddTempDesc
}

// Collect implements prometheus.Collector.
func (h *HddCollector) Collect(ch chan<- prometheus.Metric) {
	tempsString, err := h.readTempsFromConn()
	if err != nil {
		log.Printf("error reading temps from hddtemp daemon: %v", err)
		return
	}
	hddtemps, err := parseHddTemps(tempsString)
	if err != nil {
		log.Printf("error parsing temps from hddtemp daemon: %v", err)
		return
	}

	for _, ht := range hddtemps {
		ch <- prometheus.MustNewConstMetric(hddTempDesc,
			prometheus.GaugeValue,
			ht.TemperatureCelsius,
			ht.Device,
			ht.Id)
	}
}
