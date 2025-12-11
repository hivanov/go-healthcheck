package s3

import (
	"context"
	"fmt" // Re-added fmt import
	"healthcheck/core"
	//"net/url" // This import was unused and has been removed
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// mockClient implements Client for mocking in tests.
type mockClient struct {
	headBucketFunc func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
}

func (m *mockClient) HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if m.headBucketFunc != nil {
		return m.headBucketFunc(ctx, params, optFns...)
	}
	return &s3.HeadBucketOutput{}, nil // Default: successful HeadBucket
}

// setupS3Container starts a LocalStack container and returns its configuration.
func setupS3Container(tb testing.TB, ctx context.Context) (testcontainers.Container, *Config, func()) {
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

	host, err := localstackContainer.Host(ctx)
	require.NoError(tb, err, "Failed to get LocalStack host")

	port, err := localstackContainer.MappedPort(ctx, "4566/tcp")
	require.NoError(tb, err, "Failed to get LocalStack S3 mapped port")

	endpoint := fmt.Sprintf("%s:%s", host, port.Port())

	// Fixed credentials for LocalStack
	accessKeyID := "test"
	secretAccessKey := "test"
	region := "us-east-1"
	bucketName := "test-bucket"

	s3Config := &Config{
		EndpointURL:      endpoint,
		Region:           region,
		BucketName:       bucketName,
		AccessKeyID:      accessKeyID,
		SecretAccessKey:  secretAccessKey,
		DisableSSL:       true, // LocalStack typically runs without SSL by default
		S3ForcePathStyle: true, // Required for LocalStack
	}

	// Determine the scheme for the endpoint URL
	// We no longer need to parse and reconstruct. We just append the scheme directly.
	var s3EndpointURL string
	if s3Config.DisableSSL {
		s3EndpointURL = fmt.Sprintf("http://%s", endpoint)
	} else {
		s3EndpointURL = fmt.Sprintf("https://%s", endpoint)
	}
	s3Config.EndpointURL = s3EndpointURL

	// Create the bucket in LocalStack
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(s3Config.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(s3Config.AccessKeyID, s3Config.SecretAccessKey, "")),
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{
				URL:               s3Config.EndpointURL,
				HostnameImmutable: true,
				Source:            aws.EndpointSourceCustom,
				SigningRegion:     region,
				SigningName:       "s3",
			}, nil
		})),
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
