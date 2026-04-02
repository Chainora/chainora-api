# Contract Bindings

Place `abigen` generated Go bindings in this folder.

Example generation command:

```bash
abigen --abi Contract.abi --pkg bindings --type Contract --out contract.go
```

These bindings are imported by adapter/service layers when on-chain contract calls are needed.
