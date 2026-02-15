FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /autoscaler ./cmd/autoscaler/

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /autoscaler /autoscaler
EXPOSE 8080
ENTRYPOINT ["/autoscaler"]
