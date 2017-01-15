# Merge sensor-exporter (https://github.com/amkay/sensor-exporter)
# with hddtemp (https://github.com/Drewster727/hddtemp-docker)

# docker build -t epflsti/cluster.coreos.prometheus-sensors .
# docker run -i -t epflsti/cluster.coreos.prometheus-sensors bash
# docker run --privileged=true --net="host"   -v "/":"/rootfs":ro   -v "/var/run/docker.sock":"/var/run/docker.sock":rw   -v "/dev":"/dev":rw   -v "/sys":"/sys":ro  -d  --publish=9192:9255  epflsti/cluster.coreos.prometheus-sensors

# Use phusion/baseimage as base image. To make your builds
# reproducible, make sure you lock down to a specific version, not
# to `latest`! See
# https://github.com/phusion/baseimage-docker/blob/master/Changelog.md
# for a list of version numbers.
FROM phusion/baseimage:latest

# Use baseimage-docker's init system.
CMD ["/sbin/my_init"]

# Install hddtemp
RUN apt-get update && apt-get -y install \
        build-essential \
        gcc \
        libc-dev \
        hddtemp \
        lm-sensors \
        libsensors4-dev \
        git \
        golang-go

# Clean up APT when done.
RUN apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# docker run --privileged=true -d --name="hddtemp-docker" -e HDDTEMP_ARGS="-q -d -F /dev/sd*" --net="host" -e TZ="America/Chicago" -v "/var/run/docker.sock":"/var/run/docker.sock":rw -v "/dev":"/dev":rw drewster727/hddtemp-docker
# ENV HDDTEMP_ARGS="-q -d -F /dev/sd*"
# ENV TZ="Europe/London"

RUN mkdir /go
ENV GOPATH=/go

RUN go get \
        github.com/amkay/gosensors \
        github.com/prometheus/client_golang/prometheus

# Copy the local package files to the container's workspace.
ADD sensor-exporter /go/src/github.com/ncabatoff/sensor-exporter

RUN go install github.com/ncabatoff/sensor-exporter

# Run the outyet command by default when the container starts.
ENTRYPOINT [ "/bin/bash", "-c", "set -x; hddtemp -q -d -F /dev/sd* & /go/bin/sensor-exporter" ]

# Document that the service listens on port 9255.
EXPOSE 9255
