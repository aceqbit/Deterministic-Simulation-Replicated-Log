FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /seedlog ./cmd/seedlog

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /seedlog /seedlog
USER nonroot
ENTRYPOINT ["/seedlog"]
