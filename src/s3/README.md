# Amazon S3 Health Checker

This library provides a health checker component for Amazon S3, integrating with the `healthcheck/core` package. It uses the official `github.com/aws/aws-sdk-go-v2/service/s3` client library for Go.

## Health Check Details

The S3 health checker performs the following operations to determine the accessibility of a configured S3 bucket:

-   **HeadBucket Operation**: Attempts to retrieve metadata for a specific S3 bucket using the `HeadBucket` operation. A successful response indicates that the bucket exists and the provided credentials have sufficient permissions to access it. If the bucket does not exist, or permissions are insufficient, an appropriate failure status is reported.

## How to Use

The `s3` package provides functions to create a new Amazon S3 health checker component.

### `NewS3Checker`

This is the primary constructor for creating an S3 health checker.

```go
package main

import (
	"context"
	"fmt"
	"healthcheck/core"
	"healthcheck/s3"
	"time"
)

func main() {
	descriptor := core.Descriptor{
		ComponentID:   "my-s3-bucket",
		ComponentType: "storage",
		Description:   "Health check for the main S3 data bucket.",
	}

	s3Config := &s3.S3Config{
		// For AWS S3
		Region:            "us-east-1",
		BucketName:        "my-production-bucket",
		AccessKeyID:       "YOUR_AWS_ACCESS_KEY_ID",
		SecretAccessKey:   "YOUR_AWS_SECRET_ACCESS_KEY",
		// For STACKIT S3 compatible storage (or other S3 compatible services)
		// EndpointURL:       "https://object-storage.eu010.stackit.cloud",
		// S3ForcePathStyle:  true,
		// DisableSSL:        false,
	}

	// Create a new S3 checker component
	// The checker will perform HeadBucket every 5 seconds with a 3-second operation timeout.
	checker := s3.NewS3Checker(descriptor, 5*time.Second, 3*time.Second, s3Config)
	defer checker.Close()

	// You can subscribe to status changes
	statusChanges := checker.StatusChange()
	go func() {
		for status := range statusChanges {
			fmt.Printf("S3 status changed: %s - %s\n", status.Status, status.Output)
		}
	}()

	// Continuously get the health status
	for {
		health := checker.Health()
		fmt.Printf("Current S3 Health: %s - %s\n", health.Status, health.Output)
		time.Sleep(1 * time.Second)
	}
}

```

### `NewS3CheckerWithOpenS3ClientFunc` (For Advanced Usage/Testing)

This constructor allows you to inject a custom `OpenS3ClientFunc`, which is useful for mocking the S3 client in tests or for custom client initialization logic.

```go
// Example usage with a mocked S3 client
import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"healthcheck/s3"
)

type mockS3Client struct {
	headBucketFunc func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
}

func (m *mockS3Client) HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if m.headBucketFunc != nil {
		return m.headBucketFunc(ctx, params, optFns...)
	}
	return &s3.HeadBucketOutput{}, nil
}

func mockOpenS3ClientFunc(cfg *s3.S3Config) (s3.S3Client, error) { // Note: S3Client is the interface
	return &mockS3Client{}, nil
}

// checker := s3.NewS3CheckerWithOpenS3ClientFunc(descriptor, 5*time.Second, 1*time.Second, s3Config, mockOpenS3ClientFunc)
```

## Configuration Options

-   **`descriptor core.Descriptor`**: Metadata about the component (ID, type, description).
-   **`checkInterval time.Duration`**: How often the health check should be performed (e.g., `5*time.Second`).
-   **`operationTimeout time.Duration`**: The maximum time to wait for an S3 operation (e.g., `1*time.Second`).
-   **`s3Config *S3Config`**: Configuration options specific to S3 connection:
    -   `EndpointURL string`: (Optional) Custom endpoint URL for S3 compatible services (e.g., LocalStack, STACKIT Object Storage).
    -   `Region string`: AWS region (e.g., "us-east-1").
    -   `BucketName string`: The name of the S3 bucket to check.
    -   `AccessKeyID string`: AWS Access Key ID.
    -   `SecretAccessKey string`: AWS Secret Access Key.
    -   `DisableSSL bool`: (Optional) Set to `true` to disable SSL (e.g., for local development with HTTP endpoints).
    -   `S3ForcePathStyle bool`: (Optional) Set to `true` to force path style access (e.g., `http://endpoint/bucket/object` vs `http://bucket.endpoint/object`). Required for some S3-compatible services.

## Performance (SLA)

The `Health()` method of the S3 checker is designed for high-concurrency reads of the last known status. It performs very fast, typically supporting well over **200,000 calls per second** when using a mock client (which simulates a fast read of an in-memory status). Actual performance when interacting with a real S3 service will depend on network latency, S3 service load, and the specific S3 operation. The `HeadBucket` operation is generally lightweight, and the `Health()` method itself introduces minimal overhead.

## CGO Support

This library uses `github.com/aws/aws-sdk-go-v2` which is a pure Go client and **does NOT require CGO**.
