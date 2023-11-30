# Docker image based on the gitlab compiled binaries.
#
# Building the image:
#   $ docker build --build-arg varroa_version=vXX -t passelecasque/varroa:vXX .
# Or if relevant:
#   $ docker build --build-arg varroa_version=vXX -t passelecasque/varroa:vXX -t passelecasque/varroa:latest .
#
# Running the latest version:
#   $ docker run -d --name varroa --restart unless-stopped \
#        -v /path/to/config:/config \
#        -v /path/to/watch:/watch \
#        -v /path/to/downloads:/downloads \
#        passelecasque/varroa
#
FROM alpine:3.10 AS dwld
ARG varroa_version
ENV varroa_version=$varroa_version
RUN wget -q -O /tmp/varroa.zip "https://gitlab.com/passelecasque/varroa/-/jobs/artifacts/$varroa_version/download?job=compiled_varroa_released_version" \
&& \
mkdir /app \
&& \
unzip -q -d /app /tmp/varroa.zip

FROM alpine:3.10
RUN apk add --no-cache libc6-compat ca-certificates
COPY --from=dwld /app/varroa /usr/bin/varroa
VOLUME /config
VOLUME /watch
VOLUME /downloads
WORKDIR /config
CMD /usr/bin/varroa start --no-daemon

