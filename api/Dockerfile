FROM golang:1.23-alpine
RUN apk add --no-cache curl
WORKDIR /app
COPY claimsio-api .
ENTRYPOINT ["./claimsio-api"]