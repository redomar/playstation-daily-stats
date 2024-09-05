FROM golang:1.20-alpine

WORKDIR /app

# Install cron and tzdata
RUN apk add --no-cache dcron tzdata

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY *.go ./

# Build the application
RUN go build -o main .

# Copy the crontab file
COPY crontab /etc/crontabs/root

# Copy the entrypoint script
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Create a directory for the output
RUN mkdir /app/output

# Set the timezone to UTC
ENV TZ=UTC

# Use the entrypoint script
ENTRYPOINT ["/entrypoint.sh"]