# Native history hash compatibility review

Actual Claude Code `claude-opus-5-5`, effort `xhigh`, read-only frozen-source review. The completed CLI run reported the canonical model and firstParty provider, with no execution error.

**Verdict: OK to commit. I found no blockers in the delta.**

I read only `REVIEW_CONTEXT.md`, `DELTA.patch` and the two snapshot files (`shared.go`, `history_hash_test.go`). I didn't run or change anything.

**Scope**
- The change sits only in the `DownloadID` branch of `ListHistoryPage` (`shared.go:172-183`). It adds no requests, writes or new parameters, and it doesn't touch returned records.
- It checks `c.spec.Name == "sonarr" || "radarr"`, the same way the series, episode and movie guards below it check the service.

**Hex detection**
- The check is a length of 40 or 64 plus `hex.DecodeString`. Both lengths are even, so the odd-length error never comes up.
- It accepts lowercase, uppercase and mixed-case hex, and rejects anything with a non-hex byte, including multi-byte characters.
- A 40- or 64-digit ID made only of numbers counts as valid hex, but uppercasing it changes nothing, so it's harmless.
- Opaque IDs (`SABnzbd_nzo_*`, NZBGet numbers, Download Station and blackhole names) can't match, because they're the wrong length or contain non-hex characters.
- Uppercasing an ID that is already in Sonarr/Radarr's own uppercase form changes nothing, so callers that already pass the native ID behave as before.

**Query behavior**
- The change matches the live finding: the exact `downloadId` filter is case-sensitive, and both apps store torrent hashes in uppercase.
- The only effect is that lowercase qBittorrent hashes now find the history records they were missing.

**Regression quality: good, and it catches mistakes in both directions.**
- Doing nothing fails the lowercase and 64-character Sonarr/Radarr cases.
- Uppercasing everything fails Prowlarr, `SABnzbd_nzo_aBcD`, the 32-character ID and the `…z` case.
- Checking only the length fails the `…z` case.
- Handling only 40 characters fails the 64-character case.
- Lowercasing fails the mixed-case `Ab…` case.

The fake server rejects a wrongly cased filter, just like the real apps.

**Worth a quick check (not blockers)**
1. **How callers compare the IDs they get back.** Records now come back with uppercase `DownloadID` while the caller still holds the lowercase hash. If any watch or agent caller re-checks `rec.DownloadID == hash` (or uses `!=`) after the query instead of `strings.EqualFold`, it will throw those records away and the fix won't work. That code wasn't in what I was given. A grep for `DownloadID ==` / `DownloadID !=` outside `pkg/arr` would settle it.
2. **Test expected-value logic (nit).** The expected value is computed by copying the production rules (`len == 40 || 64`) plus `!strings.HasSuffix(input, "z")`, which only works because of how the inputs were picked. A table of `{spec, input, want}` pairs would make the test's assumptions clearer.
3. **Prowlarr (nit).** The test requires that Prowlarr is *not* normalized. That matches the stated scope, but it pins down a choice that the live check didn't test. That's fine if it's intentional.

## Resolution and verification

The production code and regression are both tracked in this commit. Read-only live probes verified that both installed Sonarr and Radarr return matching history for their uppercase torrent IDs and zero rows for the same lowercase filters. The regression first failed for those cases, then passed after the scoped correction; full Go race, vet, lint, gosec and actionlint checks passed. No media mutation was performed.

The watch caller explicitly compares returned download IDs with `.lower()` on both sides; its existing uppercase-history fixtures cover that behavior. Other-service and opaque IDs deliberately retain their exact original form. The regression's expected-value shortcut is restricted to its fixed input table; its non-hex and opaque cases detect overly broad normalization. The source adds no network calls or writes.
