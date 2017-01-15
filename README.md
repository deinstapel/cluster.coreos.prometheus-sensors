# sensor-exporter
Prometheus exporter for sensor data like temperature and fan speed.

[lm-sensors](http://www.lm-sensors.org) (e.g. CPU/MB temp and CPU/Chassis fan speed) and [hddtemp](http://www.guzu.net/linux/hddtemp.php) (HDD temperature from S.M.A.R.T. data) are included in the docker file.

## Dashboard
See https://grafana.net/dashboards/237 for an example dashboard.  This is probably
way more than what you want, just mine the bits that are of interest and incorporate
them into your general system health dashboard.

![dashboard sensors](epfl-sti/cluster.coreos.prometheus-sensors/raw/master/dashboard-sensors.png "dashboard sensors")


## Thanks

* https://github.com/ncabatoff/sensor-exporter
* https://github.com/amkay/sensor-exporter
  * https://hub.docker.com/r/amkay/sensor-exporter/
* https://github.com/Drewster727/hddtemp-docker
  * https://hub.docker.com/r/drewster727/hddtemp-docker
