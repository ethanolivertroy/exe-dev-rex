# VM configuration implementation slices

Goal: explain VM inputs and account capacity in the existing terminal UI.
Use live billing data rather than a hardcoded subscription table.

1. Extract resource parsing into `internal/resources`; test positive integers,
   GB/G/MB/M units, empty defaults, zero, overflow and malformed input.
   Reuse validation in settings and forms.
2. Add a read-only `billing plan --json` adapter. Decode only resource fields;
   test the observed Individual/Small response, missing fields and errors.
3. Extract resource fields from forms.go. Show live shared capacity and limits,
   units, examples and blank behavior in both forms; add a creation review.
   Refresh billing data when either form opens; degrade clearly on errors.
   Ensure clearing saved defaults really requests service defaults.

Checks: targeted tests per slice, full Go suite, vet, build and govulncheck.
No install, VM creation/deletion, app restart, or account changes are needed.

Completed: all three slices. Verified headless rendered resource forms at
60×24, 80×24 and 100×30; full tests, vet, build and govulncheck pass.
The live plan and usage queries were read-only. No real VM test was run.
