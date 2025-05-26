# Build stage
FROM golang:1.24.2-alpine AS builder

# Install required packages for building
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o aznetmon .

# Final stage - use minimal alpine image
FROM alpine:latest

# Install ca-certificates for HTTPS requests and set timezone
RUN apk --no-cache add ca-certificates tzdata

# Install wget for health checks
RUN apk --no-cache add wget

# Set working directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/aznetmon .

# Make binary executable
RUN chmod +x /app/aznetmon

# Note: Running as root is required for ICMP socket operations
# This is a necessary security trade-off for network monitoring tools

# Expose port
EXPOSE 8080

# Health check - use wget which is more reliable
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/ || exit 1

# Run the application
ENTRYPOINT ["./aznetmon"]
