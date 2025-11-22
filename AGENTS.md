# Guidelines for AI Agents Working on Collector

This document provides critical guidelines for AI agents (like Claude, GPT, etc.) working on the Collector codebase.

## ⚠️ CRITICAL: Test Requirements

### You MUST Run All Tests Before Claiming Completion

```bash
./RUN_ALL_TESTS_BEFORE_SUBMIT.sh
```

**Any test failure is YOUR responsibility to fix. NO EXCEPTIONS.**

- ❌ **NEVER** claim a task is complete if tests are failing
- ❌ **NEVER** say "these failures are pre-existing" without verification
- ❌ **NEVER** run only a subset of tests and assume everything works
- ❌ **NEVER** skip integration tests or backup validation tests

### Test Script Details

The `RUN_ALL_TESTS_BEFORE_SUBMIT.sh` script runs:

1. **Build Verification** - Ensures all packages build cleanly
2. **Code Quality Checks** - `go vet` and `go fmt` validation
3. **Unit Tests** - All package tests (`pkg/collection`, `pkg/registry`, `pkg/dispatch`, `pkg/db/sqlite`)
4. **Integration Tests** - End-to-end multi-collector scenarios
5. **Backup System Validation** - Backup functionality and near-zero downtime verification
6. **Concurrency Tests** - Race detection across all packages
7. **Durability Tests** - Database consistency and recovery
8. **Benchmarks** - Performance regression detection
9. **Coverage Report** - Test coverage metrics (should be >70%)

## Testing Philosophy

### When Making Changes

1. **Before starting work:**
   ```bash
   ./RUN_ALL_TESTS_BEFORE_SUBMIT.sh
   ```
   Establish baseline - all tests should pass

2. **During development:**
   - Run relevant package tests frequently
   - Use `go test ./pkg/collection -short` for quick feedback
   - Fix any breakage immediately

3. **Before claiming completion:**
   ```bash
   ./RUN_ALL_TESTS_BEFORE_SUBMIT.sh
   ```
   ALL tests must pass - no excuses

### If Tests Fail

1. **Investigate thoroughly** - Read the error messages
2. **Identify root cause** - Is it your change or pre-existing?
3. **Fix the issue** - Don't work around it
4. **Verify fix** - Run the full test suite again
5. **Only then** mark task as complete

### Stashing to Check Pre-Existing Issues

If you suspect a failure is pre-existing:

```bash
# Save your changes
git stash

# Run tests on clean state
./RUN_ALL_TESTS_BEFORE_SUBMIT.sh

# If tests pass, the issue IS from your changes
# If tests fail, you still need to fix them (don't make things worse)

# Restore your changes
git stash pop
```

## Code Quality Standards

### NO TODOs or FIXMEs

- ❌ Never leave `TODO` comments in code
- ❌ Never leave `FIXME` comments
- ❌ Never leave placeholder implementations
- ✅ Either implement it properly or document it as a known limitation

### Interface Usage

- ✅ Prefer interfaces over concrete types
- ✅ Use `FileSystem` interface, not `*local.FileSystem`
- ✅ Use `Store` interface, not `*sqlite.Store`
- ✅ Make code testable and mockable

### Error Handling

- ✅ Always handle errors explicitly
- ✅ Provide context in error messages
- ✅ Use `fmt.Errorf("context: %w", err)` for wrapping
- ❌ Never ignore errors with `_ = err`

### Imports

- ✅ Remove unused imports
- ✅ Group imports: stdlib, external, internal
- ✅ Run `gofmt -w` before committing

## Common Pitfalls

### 1. Filesystem vs Database Paths

```go
// ❌ WRONG: Using FS.Stat() for database files
size, err := collection.FS.Stat(ctx, collection.Store.Path())

// ✅ CORRECT: Use os.Stat() for database files
info, err := os.Stat(collection.Store.Path())
size := info.Size()
```

The `FS` interface is for file attachments, not database files.

### 2. API Signature Mismatches

```go
// ❌ WRONG: Old API signature
services, err := registryServer.ListServices(ctx, namespace)

// ✅ CORRECT: Use proper request/response messages
resp, err := registryServer.ListServices(ctx, &pb.ListServicesRequest{
    Namespace: namespace,
})
services := resp.Services
```

### 3. Proto Field Names

```go
// ❌ WRONG: Assuming field name
if resp.Valid { ... }

// ✅ CORRECT: Check the proto definition
if resp.IsValid { ... }  // Proto uses is_valid, generates IsValid
```

### 4. Type Assertions

```go
// ❌ WRONG: Hardcoded type assertion
srcFS.fs.(*local.FileSystem)

// ✅ CORRECT: Use interface
CloneCollectionFiles(ctx, srcCollection.FS, destFS, "")
```

## Integration Test Requirements

Integration tests (`pkg/integration`) validate:

- Multi-collector communication
- Registry service discovery
- Dispatcher routing
- End-to-end workflows

These tests frequently break when:
- Changing RPC signatures
- Modifying proto definitions
- Updating service interfaces

**Always run integration tests after proto or API changes.**

## Backup System Guarantees

The backup system has **proven** guarantees that must be maintained:

- **Near-zero downtime**: 6-14ms lock duration
- **Concurrent operations**: 400+ reads/sec, 25+ writes/sec during backup
- **Zero errors**: All operations succeed during backup
- **Integrity**: SQLite PRAGMA checks validate backup consistency

If backup tests fail, it means these guarantees are broken. **Fix immediately.**

## Documentation Requirements

When adding features:

1. ✅ Update relevant README files
2. ✅ Add code examples if user-facing
3. ✅ Document API changes in proto files
4. ✅ Update architecture docs if applicable
5. ❌ Don't create orphaned documentation

## Before Submitting Changes

**Checklist:**

- [ ] Run `./RUN_ALL_TESTS_BEFORE_SUBMIT.sh` - ALL tests pass
- [ ] Run `gofmt -w ./pkg/ ./cmd/` - Code is formatted
- [ ] Check `git diff` - No unintended changes
- [ ] Review all modified files - Changes are intentional
- [ ] No TODOs or FIXMEs in code
- [ ] All imports are used
- [ ] Documentation is updated
- [ ] Commit message is clear and descriptive

## For Human Reviewers

When reviewing AI-generated code:

1. ✅ Verify tests were actually run (check for test output)
2. ✅ Look for skipped tests or test modifications
3. ✅ Check for disabled error handling
4. ✅ Ensure no functionality was removed to "fix" tests
5. ✅ Validate that root causes were fixed, not symptoms

## Resources

- **Test Script**: `./RUN_ALL_TESTS_BEFORE_SUBMIT.sh`
- **Main README**: `README.md`
- **Documentation**: `docs/README.md`
- **Feature Guides**: `docs/features/`
- **Test Results**: `docs/testing/`

## Remember

> "A test failure is not an obstacle to completing your task.
> A test failure **is part of your task** - fix it!"

Any questions? Check the documentation or ask for clarification.
**Never** proceed with failing tests.
