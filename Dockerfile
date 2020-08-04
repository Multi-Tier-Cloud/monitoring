FROM alpine:latest

RUN mkdir /lib64 && ln -s /lib/libc.musl-x86_64.so.1 /lib64/ld-linux-x86-64.so.2

WORKDIR /p2p
ENV P2P_PSK=
EXPOSE 19100/tcp

COPY ping-monitor/ping-monitor .
CMD ./ping-monitor -psk $P2P_PSK

