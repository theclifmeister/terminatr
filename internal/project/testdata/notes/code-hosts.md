+++
title   = "Code hosts: GitHub, Azure DevOps"
touches = ["internal/codehost/"]
source  = "T402"
+++
## Decided
- internal/codehost Host interface; the host is picked from the origin URL (dev.azure.com, *.visualstudio.com), unknown means GitHub; a per-project override in PROJECT.md front matter (code_host, azure_url).
- Azure DevOps goes through az rest with the user's Entra login token, not the azure-devops extension; PAT fallback via AZURE_DEVOPS_EXT_PAT.
- Pull requests on Azure are completed by the coordinator only, as on GitHub.

## Watch out
- gh is blocked in the sandbox: run code host tests unsandboxed.
