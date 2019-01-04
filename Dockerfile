FROM ubuntu:18.04
MAINTAINER Luca Moser <l.moser@triumph-adler.ch>

# create client directories
RUN mkdir -p /app/assets/css && mkdir -p /app/assets/html \
&& mkdir -p /app/assets/js && mkdir -p /app/assets/img

# create server directories
RUN mkdir -p /app/configs && mkdir -p /app/logs

# copy server assets
COPY server/cmd/srv                         /app/srv
COPY server/cmd/configs/app.prod.json       /app/configs/app.json
COPY server/cmd/configs/network.prod.json   /app/configs/network.json

# copy client assets
COPY client/css/*           /app/assets/css/
COPY client/img/*           /app/assets/img/
COPY client/js/index.html   /app/assets/html/index.html
COPY client/js/app.js       /app/assets/js/app.js

# workdir and ports
WORKDIR /app
EXPOSE 9000

# entrypoint
ENTRYPOINT ["/app/srv"]