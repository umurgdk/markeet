FROM golang:1.7.3
WORKDIR /go/src/markeet
RUN go get -d -v github.com/gomodule/redigo/redis

COPY orders/main.go .
COPY orders/db.go .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o app .

FROM alpine:latest  
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=0 /go/src/markeet/app .

ENV REDIS_HOST redis
ENV STOCK_HOST markeet-stock
# PORT 8080
CMD ["./app"]  

