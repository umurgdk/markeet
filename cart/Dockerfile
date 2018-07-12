FROM golang:1.7.3
WORKDIR /go/src/markeet
RUN go get -d -v github.com/gomodule/redigo/redis

COPY cart/main.go .
COPY cart/db.go .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o app .

FROM alpine:latest  
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=0 /go/src/markeet/app .

ENV REDIS_SERVICE_HOST redis
ENV ORDERS_SERVICE_HOST markeet-orders
# PORT 8082
CMD ["./app"]  

