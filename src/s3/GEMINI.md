Create a Amazon S3 health checker, using as template the Postgres checker:
1. Create a folder src/Amazon S3
2. Create a go library there (with go.mod and friends)
3. Add the library to the @go.work file.
4. Make sure that the library depends on the official Amazon S3 client library for Go.
5. Add the core logic:
   5.1. It should keep the current status always available in Health(), and do the health check logic in separate goroutine(s).
   5.2. It should follow the pattern in @src/builtin/uptime.go and @src/postgres/checker.go .
6. Create integration tests:
  - With 100% test coverage (instead of just using unit tests) by using Testcontainers and Localstack (either the vanilla one
   or the one for Amazon S3, if such exists). 
  - Create a folder src/s3/testcontainers
  - Read the STACKIT documentation on S3 implementation here:
    - https://docs.stackit.cloud/products/storage/object-storage/
    - https://docs.stackit.cloud/products/storage/object-storage/basics/concepts/
    - https://docs.stackit.cloud/products/storage/object-storage/how-tos/basic-operations-object-storage/
    - https://docs.stackit.cloud/products/storage/object-storage/reference/error-responses-object-storage/
    - https://docs.stackit.cloud/products/storage/object-storage/reference/supported-operations-on-buckets-and-objects/
  - Create a test container (compose.yaml, s3/Containerfile and supplementary files), which mimic the minimum requirements for setting up an Amazon S3 bucket using configuration like STACKIT
7. Add fuzz tests as well.
8. Add load tests, which verify that the Health() routine can handle at least 200 calls per second using a real Amazon S3 connection (instead of
   mocking it).
9. Create a README.md file with instructions on how to run the health checker.