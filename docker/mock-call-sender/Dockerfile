FROM alpine:3.20

RUN apk add --no-cache ffmpeg curl \
    && addgroup -S mock \
    && adduser -S -G mock -u 10002 mock

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN sed -i 's/\r$//' /usr/local/bin/entrypoint.sh \
    && chmod +x /usr/local/bin/entrypoint.sh

USER mock
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
