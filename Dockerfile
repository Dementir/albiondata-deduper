FROM golang:1.8
MAINTAINER Broderick Hyman <broderickhyman@gmail.com>

ADD . /go/src/github.com/broderickhyman/albiondata-deduper
WORKDIR /go/src/github.com/broderickhyman/albiondata-deduper

RUN go get -u github.com/golang/dep/cmd/dep
RUN dep ensure
RUN go install

ENTRYPOINT /go/bin/albiondata-deduper
