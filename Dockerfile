FROM golang:1.26 AS build

WORKDIR /src
COPY . .

RUN ./test.sh \
 && CGO_ENABLED=0 make DESTDIR=/opt PREFIX=/nsq BLDFLAGS='-ldflags="-s -w"' install

FROM alpine:3.22

EXPOSE 4150 4151 4160 4161 4170 4171

# Optional volumes
# /data - used for persistent storage across reboots
# VOLUME /data
# /etc/ssl/certs - directory for SSL certificates
# VOLUME /etc/ssl/certs

COPY --from=build /opt/nsq/bin/ /usr/local/bin/
RUN ln -s /usr/local/bin/*nsq* / \
 && ln -s /usr/local/bin/*nsq* /bin/
