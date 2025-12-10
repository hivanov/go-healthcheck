Create a Amazon S3 health checker, using as template the Postgres checker.
Make sure that the library depends on the official Amazon S3 client library for Go.
Read the STACKIT documentation on S3 implementation here:
  - https://docs.stackit.cloud/products/storage/object-storage/
  - https://docs.stackit.cloud/products/storage/object-storage/basics/concepts/
  - https://docs.stackit.cloud/products/storage/object-storage/how-tos/basic-operations-object-storage/
  - https://docs.stackit.cloud/products/storage/object-storage/reference/error-responses-object-storage/
  - https://docs.stackit.cloud/products/storage/object-storage/reference/supported-operations-on-buckets-and-objects/
Create a test container (compose.yaml, s3/Containerfile and supplementary files), which mimic the minimum requirements for setting up an Amazon S3 bucket using configuration like STACKIT
