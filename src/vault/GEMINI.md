Create a Hashicorp Vault health checker, using as template the Postgres checker:
1. Create a folder src/vault
2. Create a go library there (with go.mod and friends)
3. Add the library to the @go.work file.
4. Make sure that the library depends on the official Hashicorp Vault client library for Go.
5. Add the core logic:
   5.1. It should keep the current status always available in Health(), and do the health check logic in separate goroutine(s).
   5.2. It should follow the pattern in @src/builtin/uptime.go and @src/postgres/checker.go .
6. Create integration tests with 100% test coverage (instead of just using unit tests) by using Testcontainers (either the vanilla one
   or the one for Vault, if such exists). Include also tests that connect to the vault instance defined in
   @src/vault/testcontainers/compose.yaml file (by using the correct user/password authentication mechanism described in
   @src/vault/testcontainers/vault/dev-policy.hcl ).
7. Add fuzz tests as well.
8. Add load tests, which verify that the Health() routine can handle at least 200 calls per second using real Vault connection (instead of
   mocking it).
9. Create a README.md file with instructions on how to use the component. Add technical details about the performance.