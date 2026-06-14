// Run: tsx src/__tests__/provider-model-refresh.test.ts

import { isLikelyChatModel, mergedFetchedProviderModels, providerDefaultModel, providerModelCandidates } from "../lib/providerModels";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nprovider model refresh");

eq(
  mergedFetchedProviderModels(["coding-pro"], ["coding-pro", "chat", "vision"]),
  ["coding-pro", "chat", "vision"],
  "appends discovered models without removing curated ones",
);

eq(
  mergedFetchedProviderModels(["coding-pro"], ["coding-pro", "chat", "vision"], { preserveCurated: true }),
  ["coding-pro"],
  "background refresh preserves manually curated model list",
);

eq(
  mergedFetchedProviderModels(["coding-pro"], ["chat", "vision"], { preserveCurated: true }),
  ["coding-pro"],
  "background refresh does not re-add deleted models",
);

eq(
  mergedFetchedProviderModels(["jiutian-lan-35b"], ["jiutian-lan-8b", "jiutian-lan-omni", "jiutian-lan-35b"], { preserveCurated: true }),
  ["jiutian-lan-35b"],
  "manual access refresh preserves selected MoMA model instead of importing provider catalog",
);

eq(
  providerModelCandidates(["jiutian-lan-35b"], ["jiutian-lan-8b", "jiutian-lan-omni", "jiutian-lan-35b"]),
  ["jiutian-lan-35b", "jiutian-lan-8b", "jiutian-lan-omni"],
  "manual access refresh can show provider catalog as unsaved candidates",
);

eq(
  providerModelCandidates(["jiutian-lan-35b"], ["jiutian-lan.5-asr", "jiutian-lan.5-tts", "jiutian-lan.5", "jiutian-lan-35b"]),
  ["jiutian-lan-35b", "jiutian-lan.5"],
  "manual access refresh filters non-chat candidates before saving",
);

eq(
  [
    isLikelyChatModel("jiutian-lan-35b"),
    isLikelyChatModel("jiutian-lan.5-asr"),
    isLikelyChatModel("jiutian-lan.5-tts"),
    isLikelyChatModel("text-embedding-3-small"),
  ],
  [true, false, false, false],
  "matches backend non-chat model heuristic",
);

eq(
  mergedFetchedProviderModels([], ["coding-pro", "chat"], { preserveCurated: true }),
  ["coding-pro", "chat"],
  "background refresh can populate an empty model list",
);

eq(
  providerDefaultModel("coding-pro", ["coding-pro", "chat"]),
  "coding-pro",
  "preserves current default when it remains available",
);

eq(
  providerDefaultModel("deleted", ["coding-pro", "chat"]),
  "coding-pro",
  "falls back to first saved model when default is unavailable",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
