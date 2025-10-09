FROM golang:1.23-alpine

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY *.go ./

# Build the application
RUN go build -o main .

# Create a directory for the output
RUN mkdir /app/output

# Set the timezone to UTC
ENV TZ=UTC

# Run the application
# Environment variables are passed via docker-compose.yml
CMD ["./main"]