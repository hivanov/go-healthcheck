package s3

import (
	"context"
	"fmt"
	"healthcheck/core"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// s3Client interface for mocking *s3.Client in tests.
type s3Client interface {
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	// Add other S3 operations if needed for more comprehensive checks
}

// realS3Client implements s3Client for *s3.Client.
type realS3Client struct {
	client *s3.Client
}

func (r *realS3Client) HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return r.client.HeadBucket(ctx, params, optFns...)
}

type s3Checker struct {
	checkInterval    time.Duration
	operationTimeout time.Duration
	config           *S3Config
	client           s3Client
	descriptor       core.Descriptor
	currentStatus    core.ComponentStatus
	statusChangeChan chan core.ComponentStatus
	quit             chan struct{}
	mutex            sync.RWMutex
	cancelFunc       context.CancelFunc
	ctx              context.Context
	disabled         bool
}

// S3Config holds configuration for the S3 checker.
type S3Config struct {
	EndpointURL      string
	Region           string
	BucketName       string
	AccessKeyID      string
	SecretAccessKey  string
	DisableSSL       bool
	S3ForcePathStyle bool
}

// OpenS3ClientFunc defines the signature for a function that can open an S3 client.
// This is used to allow mocking of S3 client creation in tests.
type OpenS3ClientFunc func(cfg *S3Config) (s3Client, error)

// newRealS3Client is a helper function to create a real s3Client from S3Config.
func newRealS3Client(cfg *S3Config) (s3Client, error) {
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if cfg.EndpointURL != "" {
			return aws.Endpoint{
				URI:                cfg.EndpointURL,
				HostnameImmutable:  true, // Important for custom endpoints
				Source:             aws.EndpointSourceCustom,
				URL:                cfg.EndpointURL,
				SigningRegion:      region, // Use the configured region for signing
				SigningName:        "s3",
				DisableHTTPS:       cfg.DisableSSL,
				SourceFromHostname: true,
			}, nil
		}
		// Fallback to default AWS endpoint resolver
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	awsCfg, err := config.LoadDefaultAWSConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
		config.WithEndpointResolverWithOptions(customResolver),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.S3ForcePathStyle
	})

	return &realS3Client{client: client}, nil
}

// NewS3Checker creates a new Amazon S3 health checker component.
func NewS3Checker(descriptor core.Descriptor, checkInterval, operationTimeout time.Duration, s3Config *S3Config) core.Component {
	return NewS3CheckerWithOpenS3ClientFunc(descriptor, checkInterval, operationTimeout, s3Config, newRealS3Client)
}

// NewS3CheckerWithOpenS3ClientFunc creates a new Amazon S3 health checker component,
// allowing a custom OpenS3ClientFunc to be provided for opening the S3 client.
func NewS3CheckerWithOpenS3ClientFunc(descriptor core.Descriptor, checkInterval, operationTimeout time.Duration, s3Config *S3Config, openS3Client OpenS3ClientFunc) core.Component {
	s3Conn, err := openS3Client(s3Config)
	if err != nil {
		dummyChecker := &s3Checker{
			descriptor: descriptor,
			currentStatus: core.ComponentStatus{
				Status: core.StatusFail,
				Output: fmt.Sprintf("Failed to create S3 client: %v", err),
			},
			statusChangeChan: make(chan core.ComponentStatus, 1),
			quit:             make(chan struct{}),
			ctx:              context.Background(),
			cancelFunc:       func() {},
			disabled:         false,
		}
		return dummyChecker
	}

	return newS3CheckerInternal(descriptor, checkInterval, operationTimeout, s3Config, s3Conn)
}

// newS3CheckerInternal creates a new Amazon S3 health checker component with a provided s3Client.
func newS3CheckerInternal(descriptor core.Descriptor, checkInterval, operationTimeout time.Duration, s3Config *S3Config, client s3Client) core.Component {
	ctx, cancelFunc := context.WithCancel(context.Background())

	initialStatus := core.ComponentStatus{
		Status: core.StatusWarn,
		Output: "S3 checker initializing...",
	}

	checker := &s3Checker{
		checkInterval:    checkInterval,
		operationTimeout: operationTimeout,
		config:           s3Config,
		descriptor:       descriptor,
		currentStatus:    initialStatus,
		statusChangeChan: make(chan core.ComponentStatus, 1),
		quit:             make(chan struct{}),
		ctx:              ctx,
		cancelFunc:       cancelFunc,
		disabled:         false,
		client:           client,
	}

	go checker.startHealthCheckLoop()

	return checker
}

// Close stops the checker's background operations. S3 clients do not typically need explicit closing.
func (s *s3Checker) Close() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.cancelFunc()
	close(s.quit)

	// S3 clients from aws-sdk-go-v2 generally do not need an explicit Close() call.
	// Connections are managed internally and closed when context is cancelled or client is garbage collected.
	return nil
}

// ChangeStatus updates the internal status of the component.
func (s *s3Checker) ChangeStatus(newStatus core.ComponentStatus) {
	s.updateStatus(newStatus)
}

// Disable sets the component to a disabled state.
func (s *s3Checker) Disable() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if !s.disabled {
		s.disabled = true
		s.currentStatus = core.ComponentStatus{
			Status:            core.StatusWarn,
			Time:              time.Now().UTC(),
			Output:            "S3 checker disabled",
			AffectedEndpoints: nil,
		}
		select {
		case s.statusChangeChan <- s.currentStatus:
		default:
		}
	}
}

// Enable reactivates the component.
func (s *s3Checker) Enable() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.disabled {
		s.disabled = false
		s.currentStatus = core.ComponentStatus{
			Status: core.StatusWarn,
			Output: "S3 checker enabled, re-initializing...",
		}
		select {
		case s.statusChangeChan <- s.currentStatus:
		default:
		}
		go s.performHealthCheck()
	}
}

// Status returns the current health status of the S3 component.
func (s *s3Checker) Status() core.ComponentStatus {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.currentStatus
}

// Descriptor returns the descriptor for the S3 component.
func (s *s3Checker) Descriptor() core.Descriptor {
	return s.descriptor
}

// StatusChange returns a channel that sends updates whenever the component's status changes.
func (s *s3Checker) StatusChange() <-chan core.ComponentStatus {
	return s.statusChangeChan
}

// Health returns a detailed health report for the S3 component.
func (s *s3Checker) Health() core.ComponentHealth {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var output string

	if s.currentStatus.Output != "" {
		output = s.currentStatus.Output
	}

	return core.ComponentHealth{
		ComponentID:   s.descriptor.ComponentID,
		ComponentType: s.descriptor.ComponentType,
		Status:        s.currentStatus.Status,
		Output:        output,
	}
}

// startHealthCheckLoop runs in a goroutine, periodically checking the S3 health.
func (s *s3Checker) startHealthCheckLoop() {
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	s.performHealthCheck()

	for {
		select {
		case <-ticker.C:
			s.performHealthCheck()
		case <-s.quit:
			return
		case <-s.ctx.Done():
			return
		}
	}
}

// performHealthCheck attempts to perform an S3 operation (HeadBucket) and updates the component's status.
func (s *s3Checker) performHealthCheck() {
	s.mutex.RLock()
	isDisabled := s.disabled
	s3Config := s.config // Get a copy of config for use outside mutex
	bucketName := s3Config.BucketName
	s.mutex.RUnlock()

	if isDisabled {
		return
	}

	if s.client == nil {
		s.updateStatus(core.ComponentStatus{
			Status: core.StatusFail,
			Output: "S3 client is nil",
		})
		return
	}

	checkCtx, cancelCheck := context.WithTimeout(s.ctx, s.operationTimeout)
	defer cancelCheck()

	startTime := time.Now()
	_, err := s.client.HeadBucket(checkCtx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	elapsedTime := time.Since(startTime)

	if err != nil {
		var noSuchBucket *types.NotFound
		if s3.IsAWSError(err) && err.Error() == types.NoSuchBucket.ErrorCode() || (noSuchBucket != nil && s3.ErrorCode(err) == noSuchBucket.ErrorCode()) {
			s.updateStatus(core.ComponentStatus{
				Status:        core.StatusFail,
				Output:        fmt.Sprintf("S3 bucket '%s' not found or not accessible: %v", bucketName, err),
				Time:          startTime,
				ObservedValue: elapsedTime.Seconds(),
				ObservedUnit:  "s",
			})
		} else {
			s.updateStatus(core.ComponentStatus{
				Status:        core.StatusFail,
				Output:        fmt.Sprintf("S3 HeadBucket operation failed for bucket '%s': %v", bucketName, err),
				Time:          startTime,
				ObservedValue: elapsedTime.Seconds(),
				ObservedUnit:  "s",
			})
		}
	} else {
		s.updateStatus(core.ComponentStatus{
			Status:        core.StatusPass,
			Output:        fmt.Sprintf("S3 bucket '%s' is accessible", bucketName),
			Time:          startTime,
			ObservedValue: elapsedTime.Seconds(),
			ObservedUnit:  "s",
		})
	}
}

// updateStatus safely updates the current status and notifies listeners if the status has changed.
func (s *s3Checker) updateStatus(newStatus core.ComponentStatus) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.currentStatus.Status != newStatus.Status || s.currentStatus.Output != newStatus.Output {
		s.currentStatus = newStatus
		select {
		case s.statusChangeChan <- newStatus:
		default:
			// Non-blocking send
		}
	}
}
