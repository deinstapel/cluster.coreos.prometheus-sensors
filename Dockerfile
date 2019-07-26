FROM golang:1.12-alpine as builder
ENV GO111MODULE=on
WORKDIR /workspace
RUN apk add lm_sensors-dev git build-base
ADD go.mod .
ADD go.sum .
RUN go mod download
ADD . .
RUN GOOS=linux GOARCH=amd64 go build -a -ldflags '-extldflags "-static"' -o /sensor-exporter

FROM alpine:3.10 as hddtemp-builder
RUN apk add wget build-base tar gzip gettext-dev gettext linux-headers
WORKDIR /workspace
RUN wget http://download.savannah.gnu.org/releases/hddtemp/hddtemp-0.3-beta15.tar.bz2 -O - | tar xjf -
RUN wget http://ftp.debian.org/debian/pool/main/h/hddtemp/hddtemp_0.3-beta15-53.diff.gz -O - | gunzip > patch.diff
WORKDIR /workspace/hddtemp-0.3-beta15
RUN patch -p1 -i "../patch.diff"
RUN ./configure --prefix=/usr --sbindir=/usr/bin --with-db-path="/hddtemp.db" LDFLAGS="-static"
RUN make
RUN cp debian/hddtemp.db /hddtemp.db
RUN cp src/hddtemp /hddtemp

FROM alpine:3.10
COPY --from=hddtemp-builder /hddtemp.db /
COPY --from=hddtemp-builder /hddtemp /usr/bin
COPY --from=builder /sensor-exporter /usr/bin
CMD /usr/bin/sensor-exporter
EXPOSE 9255
