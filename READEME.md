# Install Dependencies

```
go mod tidy
```

# Run

```
# Default port is 8080
go run main.go

# Custom port
go run main.go -addr :8081

# Bind IP
go run main.go -addr 127.0.0.1:8081
```

