# Redpanda Connect BigQuery Storage Write API Plugin

A robust Redpanda Connect plugin that provides high-performance streaming data ingestion to Google BigQuery using the BigQuery Storage Write API.

## Features

- **High-Performance Streaming**: Uses BigQuery Storage Write API for efficient data ingestion
- **Automatic Connection Management**: Handles 24-hour TTL expiration with automatic reconnection
- **Robust Error Handling**: Structured error detection using gRPC status codes and Google's apierror package
- **JSON to Protocol Buffer Conversion**: Seamless conversion from JSON messages to Protocol Buffer format
- **Flexible Configuration**: Support for custom credentials, batching, and data validation options
- **Production Ready**: Built-in retry logic, detailed logging, and connection pooling

## Plugin Components

This plugin provides the `gcp_bigquery_stream` output component for Redpanda Connect.

## Build and Release

### Quick Start

For development:
```sh
make build          # Build locally for testing
make dry-run        # Preview next release
```

For releases:
```sh
make release-patch  # Release v1.2.3 -> v1.2.4
make release-minor  # Release v1.2.3 -> v1.3.0
make release-major  # Release v1.2.3 -> v2.0.0
```

### Manual Build

Build the custom Redpanda Connect distribution with the BigQuery plugin:

```sh
go build
```

Build as a Docker image:

```sh
docker build . -t tubbystubby/rp-connect-bq-stream
```

### Automated Build and Release

This project includes automated build and release scripts:

#### Using the Build Script

```sh
# Interactive mode - choose version increment type
./scripts/build-and-publish.sh

# Specific version increment types
./scripts/build-and-publish.sh -t patch   # Bug fixes
./scripts/build-and-publish.sh -t minor   # New features
./scripts/build-and-publish.sh -t major   # Breaking changes

# Build locally without publishing
./scripts/build-and-publish.sh --no-push

# Preview what would happen
./scripts/build-and-publish.sh --dry-run
```

#### Using Make Targets

```sh
make help           # Show all available targets
make build          # Build locally
make release-patch  # Automated patch release
make release-minor  # Automated minor release
make release-major  # Automated major release
make clean         # Remove local images
```

#### Using Convenience Scripts

```sh
./scripts/build-local.sh     # Local development build
./scripts/release-patch.sh   # Quick patch release
./scripts/release-minor.sh   # Quick minor release
```

### What the Build Script Does

1. **Version Management**: Automatically increments semantic version tags
2. **Git Integration**: Creates and pushes version tags
3. **Docker Build**: Builds multi-architecture images with proper labels
4. **Registry Push**: Publishes to DockerHub with both versioned and `latest` tags
5. **Source Tracking**: Updates image labels to point to specific commit

### Docker Images

Published images are available at:
- `tubbystubby/rp-connect-bq-stream:latest` (latest release)
- `tubbystubby/rp-connect-bq-stream:vX.Y.Z` (specific versions)

### Prerequisites for Building

- Docker installed and running
- Git repository with proper remote setup
- DockerHub login (`docker login`) for publishing
- Go 1.23+ for manual builds

For detailed build documentation, see [BUILD.md](BUILD.md).

## Configuration

### Basic Configuration

```yaml
output:
  gcp_bigquery_stream:
    project: "my-gcp-project"
    dataset: "my_dataset"
    table: "my_table"
```

### Advanced Configuration

```yaml
output:
  gcp_bigquery_stream:
    project: "my-gcp-project"              # GCP Project ID (optional, auto-detected if not set)
    dataset: "my_dataset"                  # BigQuery Dataset ID
    table: "my_table"                      # BigQuery Table ID
    allow_partial: true                    # Allow messages with missing required fields
    discard_unknown: true                  # Ignore unknown fields and enum values
    max_in_flight: 64                      # Maximum concurrent batches
    credentials_json: "${GCP_CREDENTIALS}" # Service account credentials (optional)

    # Batching configuration
    batching:
      count: 100                           # Batch size
      period: "5s"                         # Batch timeout
      byte_size: 1048576                   # 1MB batch size limit
```

## Error Handling & Reliability

### Automatic Reconnection

The plugin automatically handles BigQuery Storage Write API connection issues:

- **TTL Expiration**: Detects and handles 24-hour connection TTL limits
- **Service Unavailable**: Reconnects during BigQuery service interruptions
- **Network Issues**: Handles transient network connectivity problems
- **Retry Logic**: Up to 2 automatic retry attempts with exponential backoff

### Supported Error Types

The plugin uses structured error detection for:

- `codes.Aborted`: Connection TTL expiration
- `codes.Unavailable`: Service unavailable, server shutdowns
- `codes.Internal`: Internal connection errors
- `codes.DeadlineExceeded`: Timeout-related issues

### Logging

Comprehensive logging provides visibility into:

- Connection status and reconnection events
- Structured error details with gRPC status codes
- BigQuery Storage-specific error information
- Batch processing statistics and performance metrics

## Data Format

### Input Format

The plugin accepts JSON messages and converts them to Protocol Buffer format:

```json
{
  "user_id": "12345",
  "event_name": "page_view",
  "timestamp": "2024-01-15T10:30:00Z",
  "properties": {
    "page": "/home",
    "referrer": "google"
  }
}
```

### Multi-line JSON

Multiple JSON objects can be sent in a single message (newline-delimited):

```json
{"user_id": "1", "event": "login"}
{"user_id": "2", "event": "signup"}
{"user_id": "3", "event": "purchase"}
```

## Usage Examples

### Basic Streaming

```yaml
input:
  kafka:
    addresses: ["localhost:9092"]
    topics: ["user_events"]

output:
  gcp_bigquery_stream:
    project: "analytics-project"
    dataset: "events"
    table: "user_activity"
```

### With Data Processing

```yaml
input:
  http_server:
    path: /webhook

pipeline:
  processors:
    - bloblang: |
        root.timestamp = now()
        root.processed_at = timestamp_unix()
        root.user_id = this.user.id
        root.event_data = this.payload

output:
  gcp_bigquery_stream:
    project: "data-warehouse"
    dataset: "raw_events"
    table: "webhook_data"
    batching:
      count: 50
      period: "2s"
```

### Multiple Outputs

```yaml
input:
  stdin: {}

output:
  switch:
    cases:
      - check: 'this.event_type == "error"'
        output:
          gcp_bigquery_stream:
            dataset: "logging"
            table: "errors"
      - check: 'this.event_type == "metric"'
        output:
          gcp_bigquery_stream:
            dataset: "monitoring"
            table: "metrics"
      - output:
          gcp_bigquery_stream:
            dataset: "events"
            table: "general"
```

## Authentication

### Default Credentials

The plugin uses Google Application Default Credentials by default:

```sh
# Set up ADC
gcloud auth application-default login
```

### Service Account Key

Provide service account credentials directly:

```yaml
output:
  gcp_bigquery_stream:
    credentials_json: |
      {
        "type": "service_account",
        "project_id": "my-project",
        "private_key_id": "...",
        "private_key": "...",
        "client_email": "...",
        "client_id": "...",
        "auth_uri": "...",
        "token_uri": "..."
      }
```

### Environment Variables

```sh
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/service-account.json"
export GOOGLE_CLOUD_PROJECT="my-gcp-project"
```

## Performance Considerations

### Batching

Configure batching for optimal performance:

- **High throughput**: Larger batch sizes (100-1000 records)
- **Low latency**: Smaller batch sizes (10-50 records) with shorter periods
- **Memory usage**: Monitor `byte_size` limits for large messages

### Connection Limits

BigQuery Storage Write API has connection limits:

- Consider using multiple streams for high-volume ingestion
- Monitor connection pool usage in production
- Use appropriate `max_in_flight` values based on your workload

## Prerequisites

- BigQuery dataset and table must exist before streaming
- Service account requires `bigquery.tables.updateData` permission
- Table schema must be compatible with your JSON message structure

## Required IAM Permissions

```json
{
  "bindings": [
    {
      "role": "roles/bigquery.dataEditor",
      "members": ["serviceAccount:your-service-account@project.iam.gserviceaccount.com"]
    }
  ]
}
```

## Troubleshooting

### Common Issues

1. **Schema Mismatch**: Ensure JSON fields match BigQuery table schema
2. **Permission Errors**: Verify service account has proper BigQuery permissions
3. **Connection Issues**: Check network connectivity and firewall rules
4. **TTL Errors**: The plugin handles these automatically, but check logs for patterns

### Debug Logging

Enable debug logging for troubleshooting:

```yaml
logger:
  level: DEBUG

output:
  gcp_bigquery_stream:
    # ... your config
```

## Version Compatibility

- **Go**: 1.23+
- **Redpanda Connect**: 4.31.0+
- **BigQuery Storage API**: v1
- **Google Cloud Go SDK**: v1.64.0+

## Contributing

This plugin is built following Redpanda Connect's plugin development guidelines. For contributions:

1. Follow the existing code structure
2. Add comprehensive error handling
3. Include appropriate logging
4. Update documentation as needed

## License

This project is licensed under the MIT License.
