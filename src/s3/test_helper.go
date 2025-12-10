package s3

import (
	"context"
	"fmt"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// mockS3Client implements s3Client for mocking in tests.
type mockS3Client struct {
	headBucketFunc func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
}

func (m *mockS3Client) HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if m.headBucketFunc != nil {
		return m.headBucketFunc(ctx, params, optFns...)
	}
	return &s3.HeadBucketOutput{}, nil // Default: successful HeadBucket
}

// setupS3Container starts a LocalStack container and returns its configuration.
func setupS3Container(tb testing.TB, ctx context.Context) (testcontainers.Container, *S3Config, func()) {
	// Create a context with a timeout for container startup
	startupCtx, startupCancel := context.WithTimeout(ctx, 2*time.Minute) // Increased timeout for container startup
	defer startupCancel()

	// Use LocalStack for S3 mocking
	req := testcontainers.ContainerRequest{
		Image:        "localstack/localstack:2.3.2", // Use a specific version for stability
		Env:          map[string]string{"SERVICES": "s3", "DEFAULT_REGION": "us-east-1"},
		ExposedPorts: []string{"4566/tcp"},
		WaitingFor:   wait.ForLog("Ready."),
	}
	localstackContainer, err := testcontainers.GenericContainer(startupCtx, testcontainers.GenericContainerRequest{ // Use startupCtx
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(tb, err, "Failed to start LocalStack container")
	require.NotNil(tb, localstackContainer, "LocalStack container is nil")

	endpoint, err := localstackContainer.Endpoint(ctx, "4566/tcp")
	require.NoError(tb, err, "Failed to get LocalStack endpoint")

	// Fixed credentials for LocalStack
	accessKeyID := "test"
	secretAccessKey := "test"
	region := "us-east-1"
	bucketName := "test-bucket"

	s3Config := &S3Config{
		EndpointURL:      endpoint,
		Region:           region,
		BucketName:       bucketName,
		AccessKeyID:      accessKeyID,
		SecretAccessKey:  secretAccessKey,
		DisableSSL:       true, // LocalStack typically runs without SSL by default
		S3ForcePathStyle: true, // Required for LocalStack
	}

	// Create the bucket in LocalStack
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URI:                s3Config.EndpointURL,
			HostnameImmutable:  true,
			Source:             aws.EndpointSourceCustom,
			URL:                s3Config.EndpointURL,
			SigningRegion:      region,
			SigningName:        "s3",
			DisableHTTPS:       s3Config.DisableSSL,
			SourceFromHostname: true,
		}, nil
	})

	awsCfg, err := config.LoadDefaultAWSConfig(ctx,
		config.WithRegion(s3Config.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(s3Config.AccessKeyID, s3Config.SecretAccessKey, "")),
		config.WithEndpointResolverWithOptions(customResolver),
	)
	require.NoError(tb, err, "Failed to load AWS config for bucket creation")

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = s3Config.S3ForcePathStyle
	})

	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(s3Config.BucketName),
	})
	require.NoError(tb, err, "Failed to create test bucket '%s'", s3Config.BucketName)

	return localstackContainer, s3Config, func() {
		if err := localstackContainer.Terminate(ctx); err != nil {
			tb.Logf("Failed to terminate LocalStack container: %v", err)
		}
	}
}

// waitForStatus helper waits for the checker to report a specific status.
func waitForStatus(tb testing.TB, checker core.Component, expectedStatus core.StatusEnum, timeout time.Duration) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	statusChangeChan := checker.StatusChange()

	// Check current status first
	currentStatus := checker.Status()
	if currentStatus.Status == expectedStatus {
		return
	}

	for {
		select {
		case <-ctx.Done():
			tb.Fatalf("Timed out waiting for status '%s'. Current status: '%s', Output: '%s'", expectedStatus, currentStatus.Status, currentStatus.Output)
		case newStatus := <-statusChangeChan:
			if newStatus.Status == expectedStatus {
				return
			}
			currentStatus = newStatus // Update current status to reflect the latest received
		case <-time.After(5 * time.Millisecond): // Small delay to avoid busy-waiting for initial status
			currentStatus = checker.Status()
			if currentStatus.Status == expectedStatus {
				return
			}
		}
	}
}
