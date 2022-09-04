FROM golang:1.19-alpine as build
WORKDIR /go/src/github.com/chinaza/scorp-vol-ext4fs

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY . .
RUN go build -o /docker-volume-ext4fs

FROM alpine
WORKDIR /
RUN apk update && apk add e2fsprogs
RUN mkdir -p /run/docker/plugins /mnt/state /mnt/volumes /mnt/fs
COPY --from=build /docker-volume-ext4fs /docker-volume-ext4fs
CMD ["/docker-volume-ext4fs"]
