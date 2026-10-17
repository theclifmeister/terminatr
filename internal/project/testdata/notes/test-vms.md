+++
title   = "Test VMs: Parallels VMs for live platform tests, the VM lock, prlctl gotchas"
touches = ["scripts/vm/", "docs/VMS.md"]
source  = "T290"
+++
## Decided
- VMs (Parallels on the Mac, all ARM64): "Windows 11", "Ubuntu 26.04 ARM64" and "macOS". Setup: scripts/vm/<os>.sh setup|sync|test.
- One thread at a time holds the VM lock; release it when done and say so in the report.
- Windows x64 only by emulation.

## Watch out
- prlctl exec runs as the VM's system user: set the home and PATH explicitly.
- A suspended VM keeps its clock behind: sync time before running tests that check certificates.
