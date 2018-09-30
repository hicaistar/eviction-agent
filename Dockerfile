FROM golang:1.10.3 as builder

ENV GOPATH /go

COPY . $GOPATH/src/eviction-agent/

WORKDIR $GOPATH/src/eviction-agent/

RUN CGO_ENABLED=0 GOOS=linux \ 
	go build -a -ldflags '-extldflags "-static"' -o eviction-agent . && \
	cp eviction-agent /bin

# The container where eviction-agent will be run 
FROM scratch
# ubuntu:16.04

COPY --from=builder /bin/eviction-agent /

ENTRYPOINT ["/eviction-agent"]