FROM containerize/dep AS builder

WORKDIR /go/src/dumper

COPY Gopkg.toml Gopkg.lock ./
RUN dep ensure -vendor-only

COPY . .

RUN go install .

FROM containerize/ssh:alpine
RUN apk add --no-cache ca-certificates mysql-client
COPY entrypoint.sh /usr/local/bin/
COPY --from=builder /go/bin/dumper /usr/local/bin/dumper

WORKDIR /home/dumper

ENTRYPOINT [ "entrypoint.sh" ]
CMD ["dumper"]