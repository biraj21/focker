# Focker

Focker is a toy container runtime written in Go, designed to create and manage lightweight Linux containers.

## Features

- Namespace Isolation: Uses Linux namespaces to isolate processes, mount points, and hostname.
- Filesystem Handling: Extracts a base Ubuntu 22.04 filesystem tarball for container use.
- Process Management: Runs specified commands inside isolated containers.

## Requirements

- Go (tested with go1.21.5)
- Linux kernel with support for namespaces (tested on Ubuntu 22.04, Pop!\_OS)
- Requires root privileges to operate due to its use of Linux namespaces.

## Usage

1. Building

   ```bash
   go build -o focker
   ```

2. Running Containers
   ```bash
   sudo ./focker run <command> [args...]
   ```
