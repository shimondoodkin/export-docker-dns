# Publishing to Docker Hub

This guide explains how to publish the DNS proxy to Docker Hub for easy distribution.

## Prerequisites

1. Docker Hub account: https://hub.docker.com/
2. Docker CLI logged in to Docker Hub
3. Built and tested DNS proxy image

## Login to Docker Hub

```bash
docker login
# Enter your Docker Hub username and password
```

## Build and Tag for Docker Hub

```bash
# Build the image with your Docker Hub username
docker build -t doodkin/export-docker-dns:latest .

# Optional: Tag with version
docker build -t doodkin/export-docker-dns:v1.0.0 .

# Or tag existing image
docker tag dns-proxy doodkin/export-docker-dns:latest
docker tag dns-proxy doodkin/export-docker-dns:v1.0.0
```

## Push to Docker Hub

```bash
# Push latest tag
docker push doodkin/export-docker-dns:latest

# Push version tag
docker push doodkin/export-docker-dns:v1.0.0
```

## Multi-Architecture Build (Optional)

For better compatibility across different platforms:

```bash
# Create and use a new builder
docker buildx create --name multiarch --use

# Build for multiple architectures
docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 \
  -t doodkin/export-docker-dns:latest \
  -t doodkin/export-docker-dns:v1.0.0 \
  --push .
```

## Automated Publishing with GitHub Actions

Create `.github/workflows/docker-publish.yml`:

```yaml
name: Build and Push Docker Image

on:
  push:
    branches: [ main ]
    tags: [ 'v*' ]
  pull_request:
    branches: [ main ]

env:
  REGISTRY: docker.io
  IMAGE_NAME: doodkin/export-docker-dns

jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write

    steps:
    - name: Checkout repository
      uses: actions/checkout@v4

    - name: Set up Docker Buildx
      uses: docker/setup-buildx-action@v3

    - name: Log in to Docker Hub
      if: github.event_name != 'pull_request'
      uses: docker/login-action@v3
      with:
        registry: ${{ env.REGISTRY }}
        username: ${{ secrets.DOCKER_USERNAME }}
        password: ${{ secrets.DOCKER_PASSWORD }}

    - name: Extract metadata
      id: meta
      uses: docker/metadata-action@v5
      with:
        images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
        tags: |
          type=ref,event=branch
          type=ref,event=pr
          type=semver,pattern={{version}}
          type=semver,pattern={{major}}.{{minor}}

    - name: Build and push Docker image
      uses: docker/build-push-action@v5
      with:
        context: .
        platforms: linux/amd64,linux/arm64
        push: ${{ github.event_name != 'pull_request' }}
        tags: ${{ steps.meta.outputs.tags }}
        labels: ${{ steps.meta.outputs.labels }}
        cache-from: type=gha
        cache-to: type=gha,mode=max
```

## GitHub Secrets Setup

Add these secrets to your GitHub repository (Settings → Secrets and variables → Actions):

- `DOCKER_USERNAME`: Your Docker Hub username
- `DOCKER_PASSWORD`: Your Docker Hub password or access token

## Usage After Publishing

Once published, users can use your image directly:

```yaml
# In docker-compose.yml
services:
  dns-proxy:
    image: doodkin/export-docker-dns:latest
    container_name: dns-proxy
    ports:
      - "127.0.0.1:5353:5353/udp"
    environment:
      - STRIP_SUFFIX=.docker
    restart: unless-stopped
```

Or with Docker run:

```bash
docker run -d --name dns-proxy -p 127.0.0.1:5353:5353/udp doodkin/export-docker-dns:latest
```

## Docker Hub Repository Setup

1. Go to https://hub.docker.com/
2. Click "Create Repository"
3. Name: `export-docker-dns`
4. Description: "DNS proxy that exports Docker's internal DNS with configurable suffix"
5. Set visibility (Public recommended for open source)
6. Link to GitHub repository for automated builds

## Best Practices

1. **Use semantic versioning**: `v1.0.0`, `v1.1.0`, etc.
2. **Always tag `latest`**: For users who want the newest version
3. **Multi-architecture builds**: Support AMD64, ARM64, and ARM v7
4. **Automated builds**: Use GitHub Actions for consistency
5. **Security scanning**: Enable Docker Hub vulnerability scanning
6. **Documentation**: Keep Docker Hub description updated

## Verification

Test your published image:

```bash
# Pull and test
docker pull doodkin/export-docker-dns:latest
docker run --rm doodkin/export-docker-dns:latest --help
```
