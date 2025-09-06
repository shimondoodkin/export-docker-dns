# DNS Proxy for Docker Container Names

A DNS proxy that exports Docker's internal DNS to the host system, allowing you to access Docker containers using custom domain names with a configurable suffix.

## The Problem

Docker automatically injects a DNS server at `127.0.0.11:53` for each Docker network. This DNS server knows about all containers in that specific network and can resolve container names to their IP addresses. However, this DNS server is **only accessible from inside Docker containers** - the host system cannot directly query it.

This creates a problem when you want to access Docker containers by name from the host system or from other containers in different networks.

## The Solution

This DNS proxy solves the problem by:

1. **Running inside a Docker container** (so it can access `127.0.0.11:53`)
2. **Exposing a DNS server on the host** (via port mapping)
3. **Proxying DNS queries** from the host to Docker's internal DNS
4. **Adding a configurable suffix** (like `.docker`) to distinguish Docker queries

## Features

- **Docker DNS Integration**: Queries Docker's internal DNS (127.0.0.11:53) for container names
- **Suffix Stripping**: Removes configurable suffix (default: `.docker`) before querying Docker DNS
- **Optional Upstream Fallback**: Can optionally fall back to upstream DNS for non-Docker queries (disabled by default)
- **Configurable**: All settings can be configured via environment variables
- **Metrics**: Optional query and error metrics logging
- **Graceful Shutdown**: Handles SIGTERM and SIGINT signals properly

## How it Works

1. Client queries `mycontainer.docker` → DNS Proxy (running in Docker)
2. Proxy strips `.docker` suffix → queries Docker DNS (`127.0.0.11:53`) for `mycontainer`
3. Docker DNS returns container IP → Proxy returns response with original domain name
4. Non-Docker queries return NXDOMAIN (unless upstream DNS is enabled)

## Configuration

All configuration is done via environment variables. **All variables have sensible defaults**, so you can run the DNS proxy without specifying any environment variables.

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `LISTEN_ADDR` | `0.0.0.0` | Address to listen on |
| `LISTEN_PORT` | `5353` | Port to listen on |
| `DOCKER_DNS` | `127.0.0.11:53` | Docker's internal DNS server |
| `UPSTREAM_DNS` | `8.8.8.8:53` | Upstream DNS server for non-Docker queries |
| `ENABLE_UPSTREAM` | `false` | Enable upstream DNS fallback for non-Docker queries |
| `TIMEOUT_SECONDS` | `2` | DNS query timeout in seconds |
| `LOG_LEVEL` | `INFO` | Log level (DEBUG, INFO, ERROR) |
| `ENABLE_METRICS` | `false` | Enable periodic metrics logging |
| `STRIP_SUFFIX` | `.docker` | Suffix to strip before querying Docker DNS |

## Usage

### Docker Build and Run

```bash
# Build the image
docker build -t dns-proxy .

# Run the container
docker run -d --name dns-proxy -p 5353:5353/udp dns-proxy
```

### Docker Compose

Basic usage:

```bash
# Start the service
docker-compose up -d

# View logs
docker-compose logs -f dns-proxy

# Stop the service
docker-compose down
```

### Real-World Example: Integration with Existing Services

Here's how to integrate the DNS proxy with an existing docker-compose setup (like nginx-proxy-manager):

```yaml
version: "3.8"
services:
  # Your existing services
  nginx:
    container_name: nginx
    image: 'jc21/nginx-proxy-manager:latest'
    restart: unless-stopped
    extra_hosts:
      - "host.docker.internal:host-gateway"
    ports:
      - '80:80'
      - '443:443'
    volumes:
      - ./data:/data
      - ./letsencrypt:/etc/letsencrypt
    networks:
      - nginx

  # Add the DNS proxy to export Docker DNS
  dns-proxy:
    image: doodkin/export-docker-dns:latest
    container_name: dns-proxy
    ports:
      - "127.0.0.1:5353:5353"  # Only bind to localhost
      - "127.0.0.1:5353:5353/udp"  # Only bind to localhost
    environment:
      - STRIP_SUFFIX=.docker
    restart: unless-stopped
    networks:
      - nginx  # Same network as your other services

networks:
  nginx:
    name: nginx
    driver: bridge
```

After starting this setup:
- Configure systemd-resolved as shown above
- Access nginx container: `curl http://nginx.docker`
- The DNS proxy will resolve `nginx.docker` to the container's IP in the `nginx` network

### Testing

```bash
# Test Docker container resolution
dig @localhost -p 5353 mycontainer.docker

# Test upstream DNS resolution (only works if ENABLE_UPSTREAM=true)
dig @localhost -p 5353 google.com
```

### Configuring systemd-resolved for Automatic Suffix Routing



To automatically route all `.docker` queries to your DNS proxy, configure systemd-resolved:

Create a .conf file in /etc/systemd/resolved.conf.d/

for example  /etc/systemd/resolved.conf.d/docker-dns.conf
with content

```
[Resolve]
DNS=127.0.0.1:5353~docker
```

or using commands:

```bash
# Create a resolved configuration for the .docker domain
sudo mkdir -p /etc/systemd/resolved.conf.d
sudo tee /etc/systemd/resolved.conf.d/docker-dns.conf << EOF
[Resolve]
DNS=127.0.0.1:5353~docker
EOF
```

restart systemd-resolved

```
# Restart systemd-resolved
sudo systemctl restart systemd-resolved

# Verify the configuration
resolvectl status
```

After this configuration:
- Queries for `*.docker` domains will automatically go to your DNS proxy
- Other queries will use your system's default DNS servers
- You can access containers directly: `ping mycontainer.docker`

### Alternative: Manual DNS Configuration

If you don't use systemd-resolved, you can configure your DNS manually:

```bash
# Add to /etc/resolv.conf (temporary - will be overwritten)
nameserver 127.0.0.1:5353

# Or use dnsmasq to route specific domains
# Add to /etc/dnsmasq.conf:
server=/docker/127.0.0.1#5353
```

## Example Use Cases

1. **Development Environment**: Access containers by name with custom domains
2. **Service Discovery**: Provide DNS-based service discovery for Docker containers
3. **Load Balancer Integration**: Use with load balancers that need DNS resolution
4. **Local Development**: Simplify container access in development workflows

## Logs

The proxy logs all queries with configurable verbosity:

```
[INFO] Query #1 for: mycontainer.docker. (type: A) from 172.17.0.1:54321
[DEBUG] Stripping suffix '.docker' from 'mycontainer.docker.', querying Docker DNS for: mycontainer
[DEBUG] Got 1 answers from Docker DNS for mycontainer
```

## Publishing to Docker Hub

To publish this DNS proxy to Docker Hub:

```bash
# 1. Build and tag the image
docker build -t yourusername/dns-proxy:latest .

# 2. Tag with version (optional)
docker tag yourusername/dns-proxy:latest yourusername/dns-proxy:v1.0.0

# 3. Login to Docker Hub
docker login

# 4. Push the image
docker push yourusername/dns-proxy:latest
docker push yourusername/dns-proxy:v1.0.0  # if you tagged with version

# 5. Update your docker-compose.yml to use the published image
# Replace:
#   build: ./export-docker-dns
# With:
#   image: yourusername/dns-proxy:latest
```

### Using Published Image

Once published, users can use your DNS proxy without building locally:

```yaml
services:
  dns-proxy:
    image: yourusername/dns-proxy:latest  # Use published image
    container_name: dns-proxy
    ports:
      - "127.0.0.1:5353:5353"
      - "127.0.0.1:5353:5353/udp"
    environment:
      - STRIP_SUFFIX=.docker
    restart: unless-stopped
    networks:
      - your-network
```

## Security Notes

- Runs as non-root user in distroless container
- Only forwards DNS queries, no other network access
- Configurable timeouts prevent hanging queries
- No persistent storage or state
