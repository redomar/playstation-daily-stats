FROM golang:1.20-alpine

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

# if .env file exists, copy it to the container
COPY .env /app/.env

# Run the application
CMD ["./main"]