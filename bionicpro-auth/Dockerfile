FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /out/bionicpro-auth .

FROM alpine:3.19
RUN adduser -D -H appuser
COPY --from=build /out/bionicpro-auth /usr/local/bin/bionicpro-auth
USER appuser
EXPOSE 8000
ENTRYPOINT ["/usr/local/bin/bionicpro-auth"]
