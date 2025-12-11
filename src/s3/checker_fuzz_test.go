package s3

import (
	"context"
	"fmt"
	"healthcheck/core"
	"net/url" // For URL parsing validation
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
)

func FuzzS3Checker_NewS3Checker(f *testing.F) {
	// Seed corpus with various S3 configuration parameters
	f.Add("http://localhost:4566", "us-east-1", "test-bucket", "accesskey", "secretkey", true, true, int64(50), int64(500)) // Valid localstack config
	f.Add("", "us-west-2", "another-bucket", "ak", "sk", false, false, int64(10), int64(100))                               // AWS default endpoint
	f.Add("invalid-url", "us-east-1", "mybucket", "ak", "sk", true, true, int64(100), int64(100))                           // Invalid endpoint URL
	f.Add("http://localhost:4566", "", "test-bucket", "ak", "sk", true, true, int64(1), int64(1))                           // Empty region
	f.Add("http://localhost:4566", "us-east-1", "", "ak", "sk", true, true, int64(0), int64(0))                             // Empty bucket name, zero intervals
	f.Add("http://localhost:4566", "us-east-1", "test-bucket", "", "sk", true, true, int64(-1), int64(-1))                  // Empty access key, negative intervals

	f.Fuzz(func(t *testing.T, endpointURL, region, bucketName, accessKeyID, secretAccessKey string, disableSSL, forcePathStyle bool, checkIntervalMs, operationTimeoutMs int64) {
		descriptor := core.Descriptor{
			ComponentID:   "fuzz-s3",
			ComponentType: "s3",
			Description:   "Fuzz test for S3 checker",
		}

		// Ensure intervals are positive
		if checkIntervalMs <= 0 {
			checkIntervalMs = 1
		}
		checkInterval := time.Duration(checkIntervalMs) * time.Millisecond

		if operationTimeoutMs <= 0 {
			operationTimeoutMs = 1
		}
		operationTimeout := time.Duration(operationTimeoutMs) * time.Millisecond

		s3Config := &Config{
			EndpointURL:      endpointURL,
			Region:           region,
			BucketName:       bucketName,
			AccessKeyID:      accessKeyID,
			SecretAccessKey:  secretAccessKey,
			DisableSSL:       disableSSL,
			S3ForcePathStyle: forcePathStyle,
		}

		// Mock OpenS3ClientFunc to simulate client creation success/failure
		mockOpenS3Client := func(cfg *Config) (Client, error) {
			// Basic validation for EndpointURL to simulate real client behavior
			if cfg.EndpointURL != "" {
				_, err := url.ParseRequestURI(cfg.EndpointURL)
				if err != nil {
					return nil, fmt.Errorf("invalid endpoint URL: %w", err)
				}
			}
			if cfg.Region == "" && cfg.EndpointURL == "" { // Region is needed for AWS default, or endpoint URL for custom
				return nil, fmt.Errorf("region and endpoint cannot both be empty")
			}
			if cfg.BucketName == "" {
				return nil, fmt.Errorf("bucket name cannot be empty")
			}

			// Simulate successful client creation, but with mock HeadBucket behavior
			mockClient := &mockClient{
				headBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
					// Simulate different errors based on bucket name or config
					if *params.Bucket == "non-existent-bucket" {
						return nil, &types.NotFound{}
					}
					if cfg.AccessKeyID == "invalid" {
						return nil, fmt.Errorf("access denied") // Generic permission error
					}
					return &s3.HeadBucketOutput{}, nil // Simulate success
				},
			}
			return mockClient, nil
		}

		checker := NewS3CheckerWithOpenS3ClientFunc(descriptor, checkInterval, operationTimeout, s3Config, mockOpenS3Client)
		defer func() {
			if err := checker.Close(); err != nil {
				t.Errorf("Checker Close() returned an unexpected error: %v", err)
			}
		}()

		if checker == nil {
			t.Skipf("NewS3Checker returned nil for config: %+v", s3Config)
			return
		}

		// Exercise other methods
		_ = checker.Status()
		_ = checker.Descriptor()
		_ = checker.Health()
		checker.Disable()
		checker.Enable()
		checker.ChangeStatus(core.ComponentStatus{Status: core.StatusFail, Output: "fuzz"})

		// Allow some time for goroutines to process if necessary
		time.Sleep(checkInterval * 2)

		status := checker.Status()
		// Depending on the config, status could be Warn (initializing/disabled) or Fail (connection/operation error).
		// We primarily want to ensure it doesn't crash.
		assert.True(t, status.Status == core.StatusWarn || status.Status == core.StatusFail || status.Status == core.StatusPass,
			"Unexpected status %s for config: %+v, output: %s", status.Status, s3Config, status.Output)
	})
}
