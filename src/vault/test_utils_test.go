package vault_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hashicorp/vault/api"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	vaultPort       = "8200/tcp"
	vaultRootToken  = "myroot" // Matches the token set in the Dockerfile
	vaultPolicyName = "dev-policy"
)

var (
	GlobalVaultContainer testcontainers.Container
	GlobalVaultClient    *api.Client
	GlobalVaultAddr      string
)

// SetupVaultContainerForTests starts a Vault container and configures a client for testing.
// It returns the container, Vault address, and a configured API client.
func SetupVaultContainerForTests(ctx context.Context) (testcontainers.Container, string, *api.Client, error) {
	pwd, err := os.Getwd()
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to get current working directory: %v", err)
	}
	vaultDockerfilePath := fmt.Sprintf("%s/testcontainers/vault", pwd)
	devPolicyPath := fmt.Sprintf("%s/testcontainers/vault/dev-policy.hcl", pwd)

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    vaultDockerfilePath,
			Dockerfile: "Containerfile",
		},
		ExposedPorts: []string{vaultPort},
		WaitingFor:   wait.ForHTTP("/v1/sys/health").WithPort(vaultPort).WithStartupTimeout(5 * time.Minute),
		Env: map[string]string{
			"VAULT_DEV_ROOT_TOKEN_ID": vaultRootToken,
		},
	}

	vaultC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to start Vault container: %v", err)
	}

	ip, err := vaultC.Host(ctx)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to get Vault container IP: %v", err)
	}

	port, err := vaultC.MappedPort(ctx, vaultPort)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to get Vault container port: %v", err)
	}

	vaultAddr := fmt.Sprintf("http://%s:%s", ip, port.Port())
	log.Printf("Vault running at: %s", vaultAddr)

	// Initialize Vault API client
	config := api.DefaultConfig()
	config.Address = vaultAddr
	client, err := api.NewClient(config)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to create Vault API client: %v", err)
	}
	client.SetToken(vaultRootToken)

	// Wait for Vault to be unsealed and ready
	err = waitForVaultReady(ctx, client)
	if err != nil {
		return nil, "", nil, fmt.Errorf("Vault did not become ready: %v", err)
	}
	log.Println("Vault is unsealed and ready.")

	// Read policy content from file
	policyContentBytes, err := os.ReadFile(devPolicyPath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to read policy file %s: %v", devPolicyPath, err)
	}
	policyContent := string(policyContentBytes)

	// Apply policy for testing
	err = client.Sys().PutPolicy(vaultPolicyName, policyContent)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to put policy %s: %v", vaultPolicyName, err)
	}
	log.Printf("Policy '%s' applied.", vaultPolicyName)

	// Enable userpass auth method
	err = client.Sys().EnableAuthWithOptions("userpass", &api.EnableAuthOptions{
		Type: "userpass",
	})
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to enable userpass auth method: %v", err)
	}
	log.Println("Userpass auth method enabled.")

	// Create a test user and associate the policy
	userpassMountPath := "auth/userpass/users/boo-user"
	userData := map[string]interface{}{
		"password": "boo-password",
		"policies": []string{vaultPolicyName},
	}
	_, err = client.Logical().Write(userpassMountPath, userData)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to create test user: %v", err)
	}
	log.Println("Test user 'boo-user' created with 'dev-policy'.")

	return vaultC, vaultAddr, client, nil
}

// waitForVaultReady waits for the Vault instance to report as initialized, unsealed, and not in standby.
func waitForVaultReady(ctx context.Context, client *api.Client) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timed out waiting for Vault to be ready: %w", timeoutCtx.Err())
		case <-ticker.C:
			health, err := client.Sys().Health()
			if err != nil {
				if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
					log.Printf("Vault health check network timeout, retrying: %v", err)
					continue
				}
				log.Printf("Vault health check error, retrying: %v", err)
				continue
			}

			if health != nil && health.Initialized && !health.Sealed && !health.Standby {
				return nil
			}
			log.Printf("Waiting for Vault to be ready... Current health: Initialized=%t, Sealed=%t, Standby=%t, Error=%v", health.Initialized, health.Sealed, health.Standby, err)
		}
	}
}
