# build:
# docker build --tag=netdata-statsd .
#
FROM ubuntu:latest
RUN apt-get update && apt-get install -qq -y apt-utils zlib1g-dev uuid-dev libmnl-dev gcc make git autoconf autoconf-archive autogen automake pkg-config curl
WORKDIR ./netdata
RUN git clone https://github.com/netdata/netdata --depth=100 .
RUN ./netdata-installer.sh --dont-wait --dont-start-it
ADD ./netdata.conf /etc/netdata/netdata.conf
EXPOSE 19999 8125 8125/udp 8126
#
# run:
# docker run --rm -it -p 8125:8125/udp -p 19999:19999 netdata-statsd
#
# to start netdata (manual):
# /usr/sbin/netdata
# OR 
# netdata start
# to kill:
# killall netdata 
