#!/bin/bash
#
# sparkdream-jackal-upload.sh
#
# Uploads archived block batches to Jackal Protocol decentralized storage.
# Supports two upload modes:
#
#   vault  - Direct on-chain storage via @jackallabs/jackal.js SDK.
#            Uses your wallet mnemonic (signed locally, never sent to servers).
#            Requires a one-time storage plan purchase and vault setup at
#            https://vault.jackalprotocol.com
#
#   pin    - Hosted IPFS pinning via the Jackal Pin API.
#            Uses an API key (monthly billed service).
#            Get a key at https://pin.jackalprotocol.com/keys
#
# Jackal is a Cosmos SDK chain with proof-of-persistence storage. Files
# are replicated across 3+ storage providers who must periodically prove
# they still hold the data or face penalties, so archives are actively
# maintained (unlike cold storage).
#
# NOTE: This script is intended to be run from your local machine
# (not inside the container). Do not store mnemonics or API keys on
# Akash providers.
#
# Prerequisites (vault mode):
#   1. Install Node.js 20+ and the Jackal SDK:
#      npm install -g @jackallabs/jackal.js
#
#   2. A funded Jackal wallet (JKL tokens) with an active storage plan.
#      Set up your vault at: https://vault.jackalprotocol.com
#
#   3. Set your wallet mnemonic:
#      export JACKAL_MNEMONIC="your 24 word mnemonic ..."
#
# Prerequisites (pin mode):
#   1. Create an account and purchase a plan at:
#      https://pin.jackalprotocol.com
#
#   2. Generate an API key at:
#      https://pin.jackalprotocol.com/keys
#
#   3. Set your API key:
#      export JACKAL_API_KEY="your-api-key"
#
# Usage:
#   ./sparkdream-jackal-upload.sh [archive_directory]
#
#   Upload to a subfolder (vault mode):
#     JACKAL_FOLDER=my-archives ./sparkdream-jackal-upload.sh
#
#   Delete a vault folder:
#     ./sparkdream-jackal-upload.sh delete-folder <folder_name>
#
#   List vault folders:
#     ./sparkdream-jackal-upload.sh list-folders
#
#   Delete all files from the vault folder and reset manifest/tracker,
#   so you can re-upload fresh:
#     ./sparkdream-jackal-upload.sh clean [archive_directory]
#
#   Rebuild manifest from remote (vault mode only).
#   Useful when an upload timed out but the files were actually stored
#   on the remote server — reconciles the local manifest and tracker
#   with what is actually present in the vault:
#     ./sparkdream-jackal-upload.sh fix-manifest [archive_directory]
#
# Environment variables:
#   JACKAL_MODE       - Upload mode: "vault" or "pin" (default: auto-detect)
#   JACKAL_MNEMONIC   - 24-word wallet mnemonic (vault mode)
#   JACKAL_API_KEY    - Jackal Pin API key (pin mode)
#   ARCHIVE_DIR       - Directory containing .jsonl.gz files (default: ./sparkdream-archives)
#   MANIFEST_FILE     - Path to the manifest (default: $ARCHIVE_DIR/jackal-manifest.csv)
#   UPLOADED_FILE     - Tracks already-uploaded files (default: $ARCHIVE_DIR/.jackal-uploaded)
#   JACKAL_RPC        - Jackal RPC endpoint (default: https://rpc.jackalprotocol.com, vault mode)
#   JACKAL_REST       - Jackal REST endpoint (default: https://api.jackalprotocol.com, vault mode)
#   JACKAL_API_URL    - Jackal Pin API base URL (default: https://pinapi.jackalprotocol.com, pin mode)
#   JACKAL_FOLDER     - Vault subfolder under Home to upload into (default: sparkdream-archives, vault mode)
#                       Set to "Home" to upload directly to the root directory.
#   DRY_RUN           - Set to "true" to show what would be uploaded without uploading
#
set -e

# ---------------------------------------------------------------------------
# Vault management subcommands (delete-folder, list-folders)
# ---------------------------------------------------------------------------
JACKAL_VAULT_JS='
const mnemonic = process.env.JACKAL_MNEMONIC;
const rpc = process.env.JACKAL_RPC || "https://rpc.jackalprotocol.com";
const rest = process.env.JACKAL_REST || "https://api.jackalprotocol.com";

async function connectStorage() {
    const { ClientHandler } = require("@jackallabs/jackal.js");
    const client = await ClientHandler.connect({
        selectedWallet: "mnemonic",
        mnemonic,
        chainId: "jackal-1",
        endpoint: rpc,
        chainConfig: {
            chainId: "jackal-1", chainName: "Jackal Mainnet", rpc, rest,
            bip44: { coinType: 118 },
            stakeCurrency: { coinDenom: "JKL", coinMinimalDenom: "ujkl", coinDecimals: 6 },
            bech32Config: {
                bech32PrefixAccAddr: "jkl", bech32PrefixAccPub: "jklpub",
                bech32PrefixValAddr: "jklvaloper", bech32PrefixValPub: "jklvaloperpub",
                bech32PrefixConsAddr: "jklvalcons", bech32PrefixConsPub: "jklvalconspub",
            },
            currencies: [{ coinDenom: "JKL", coinMinimalDenom: "ujkl", coinDecimals: 6 }],
            feeCurrencies: [{ coinDenom: "JKL", coinMinimalDenom: "ujkl", coinDecimals: 6,
                gasPriceStep: { low: 0.002, average: 0.002, high: 0.02 } }],
            features: [],
        },
    });
    const storage = await client.createStorageHandler();
    await storage.loadProviderPool();
    await storage.upgradeSigner();
    await storage.initStorage();
    await storage.loadDirectory({ path: "Home" });
    return storage;
}
'

if [ "$1" = "list-folders" ]; then
    if [ -z "$JACKAL_MNEMONIC" ]; then
        echo "ERROR: JACKAL_MNEMONIC is required." >&2
        exit 1
    fi
    export NODE_PATH="${NODE_PATH:+${NODE_PATH}:}$(npm root -g)"
    node -e "${JACKAL_VAULT_JS}
(async () => {
    const storage = await connectStorage();
    const folders = storage.listChildFolders();
    if (folders.length === 0) {
        console.log('No folders in Home.');
    } else {
        console.log('Folders in Home:');
        for (const f of folders) {
            console.log('  ' + (f || '(empty name)'));
        }
    }
    process.exit(0);
})().catch(err => { console.error('Error: ' + err.message); process.exit(1); });
"
    exit $?
fi

if [ "$1" = "delete-folder" ]; then
    FOLDER_NAME="$2"
    if [ -z "$FOLDER_NAME" ]; then
        echo "Usage: $0 delete-folder <folder_name>" >&2
        echo "  Use 'list-folders' to see available folders." >&2
        exit 1
    fi
    if [ -z "$JACKAL_MNEMONIC" ]; then
        echo "ERROR: JACKAL_MNEMONIC is required." >&2
        exit 1
    fi
    export NODE_PATH="${NODE_PATH:+${NODE_PATH}:}$(npm root -g)"
    echo "Deleting folder: Home/${FOLDER_NAME}..."
    node -e "${JACKAL_VAULT_JS}
const target = process.argv[1];
(async () => {
    const storage = await connectStorage();
    await storage.deleteTargets({ targets: [target] });
    console.log('Deleted: Home/' + target);
    process.exit(0);
})().catch(err => { console.error('Error: ' + err.message); process.exit(1); });
" "$FOLDER_NAME"
    exit $?
fi

if [ "$1" = "clean" ]; then
    CLEAN_ARCHIVE_DIR="${2:-${ARCHIVE_DIR:-./sparkdream-archives}}"
    CLEAN_MANIFEST="${MANIFEST_FILE:-${CLEAN_ARCHIVE_DIR}/jackal-manifest.csv}"
    CLEAN_UPLOADED="${UPLOADED_FILE:-${CLEAN_ARCHIVE_DIR}/.jackal-uploaded}"

    echo "Resetting local upload state..."
    echo "  Manifest: $CLEAN_MANIFEST"
    echo ""

    # If mnemonic is set, also delete files from vault
    if [ -n "$JACKAL_MNEMONIC" ]; then
        JACKAL_FOLDER="${JACKAL_FOLDER:-sparkdream-archives}"
        export JACKAL_RPC="${JACKAL_RPC:-https://rpc.jackalprotocol.com}"
        export JACKAL_REST="${JACKAL_REST:-https://api.jackalprotocol.com}"
        export NODE_PATH="${NODE_PATH:+${NODE_PATH}:}$(npm root -g)"

        echo "Also deleting files from vault: Home/${JACKAL_FOLDER}"
        echo "  (Skip vault deletion by unsetting JACKAL_MNEMONIC)"
        echo ""

        JACKAL_FOLDER="$JACKAL_FOLDER" node -e "
const _stderr = process.stderr;
['log','warn','dir','error','info','debug'].forEach(m => {
    console[m] = (...args) => { _stderr.write(require('util').format(...args) + '\n'); };
});
process.on('unhandledRejection', (err) => {
    if (err?.txId || (err?.message || '').includes('was submitted but was not yet found')) {
        console.log('Tx confirmation timed out — continuing...');
    } else {
        console.error('Unhandled: ' + (err?.message || err));
    }
});
${JACKAL_VAULT_JS}
const folder = process.env.JACKAL_FOLDER || 'sparkdream-archives';
(async () => {
    const storage = await connectStorage();
    const existing = storage.listChildFolders();
    if (!existing.includes(folder)) {
        process.stdout.write('Folder not found — skipping vault cleanup.\n');
        process.exit(0);
    }
    await storage.loadDirectory({ path: 'Home/' + folder });
    const files = storage.listChildFiles();
    if (files.length === 0) {
        process.stdout.write('Vault folder already empty.\n');
        process.exit(0);
    }
    process.stdout.write('Deleting ' + files.length + ' file(s)...\n');
    // Delete all files in one tx with a safety timeout (SDK hangs on WebSocket)
    const CLEAN_TIMEOUT_MS = 120000;
    await Promise.race([
        (async () => {
            try {
                await storage.deleteTargets({ targets: files });
                process.stdout.write('Deleted all files.\n');
            } catch (err) {
                if (err?.txId || (err?.message || '').includes('was submitted but was not yet found')) {
                    process.stdout.write('Delete tx submitted (confirmation timed out — likely succeeded).\n');
                } else {
                    process.stdout.write('WARNING: ' + err.message + '\n');
                }
            }
        })(),
        new Promise((resolve) => setTimeout(() => {
            process.stdout.write('Delete tx submitted (safety timeout) — likely succeeded.\n');
            resolve();
        }, CLEAN_TIMEOUT_MS)),
    ]);
    process.exit(0);
})().catch(err => { process.stderr.write('Error: ' + err.message + '\n'); process.exit(1); });
" || echo "  Vault cleanup had errors (files may already be deleted)."
        echo ""
    else
        echo "  JACKAL_MNEMONIC not set — skipping vault deletion, only resetting local state."
        echo ""
    fi

    # Reset manifest and tracker
    echo "file,from_block,to_block,jackal_path,cid,file_size,uploaded_at" > "$CLEAN_MANIFEST"
    : > "$CLEAN_UPLOADED"
    echo "Manifest and upload tracker cleared."
    echo ""
    echo "========================================"
    echo "Clean complete — ready to re-upload"
    echo "========================================"
    exit 0
fi

if [ "$1" = "fix-manifest" ]; then
    FIX_ARCHIVE_DIR="${2:-${ARCHIVE_DIR:-./sparkdream-archives}}"
    FIX_MANIFEST="${MANIFEST_FILE:-${FIX_ARCHIVE_DIR}/jackal-manifest.csv}"
    FIX_UPLOADED="${UPLOADED_FILE:-${FIX_ARCHIVE_DIR}/.jackal-uploaded}"
    JACKAL_FOLDER="${JACKAL_FOLDER:-sparkdream-archives}"

    if [ -z "$JACKAL_MNEMONIC" ]; then
        echo "ERROR: JACKAL_MNEMONIC is required (fix-manifest is vault mode only)." >&2
        exit 1
    fi
    export JACKAL_RPC="${JACKAL_RPC:-https://rpc.jackalprotocol.com}"
    export JACKAL_REST="${JACKAL_REST:-https://api.jackalprotocol.com}"
    export NODE_PATH="${NODE_PATH:+${NODE_PATH}:}$(npm root -g)"

    echo "Connecting to Jackal vault to list remote files..."
    echo "Folder: Home/${JACKAL_FOLDER}"
    echo ""

    REMOTE_FILES=$(JACKAL_FOLDER="$JACKAL_FOLDER" node -e "
// Redirect all console output to stderr so SDK debug noise does not
// mix with our clean filename output on stdout.
const _stderr = process.stderr;
['log','warn','dir','error','info','debug'].forEach(m => {
    console[m] = (...args) => { _stderr.write(require('util').format(...args) + '\n'); };
});

${JACKAL_VAULT_JS}
const folder = process.env.JACKAL_FOLDER || 'sparkdream-archives';
(async () => {
    const storage = await connectStorage();
    if (folder !== 'Home') {
        const existing = storage.listChildFolders();
        if (!existing.includes(folder)) {
            console.error('ERROR: Folder \"' + folder + '\" does not exist on the vault.');
            process.exit(1);
        }
        await storage.loadDirectory({ path: 'Home/' + folder });
    }
    const files = storage.listChildFiles();
    for (const f of files) {
        // Only output block archive files
        if (/^blocks_\d+_to_\d+\.jsonl\.gz$/.test(f)) {
            process.stdout.write(f + '\n');
        }
    }
    process.exit(0);
})().catch(err => { console.error('Error: ' + err.message); process.exit(1); });
") || {
        echo "ERROR: Failed to list remote files." >&2
        exit 1
    }

    if [ -z "$REMOTE_FILES" ]; then
        echo "No block archive files found in vault folder."
        exit 0
    fi

    # Rebuild manifest and uploaded tracker
    VAULT_PREFIX="Home/${JACKAL_FOLDER}"
    if [ "$JACKAL_FOLDER" = "Home" ]; then
        VAULT_PREFIX="Home"
    fi

    echo "file,from_block,to_block,jackal_path,file_size,uploaded_at" > "$FIX_MANIFEST"
    : > "$FIX_UPLOADED"

    TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    FILE_COUNT=0

    echo "$REMOTE_FILES" | sort -t_ -k2 -n | while IFS= read -r FILENAME; do
        [ -z "$FILENAME" ] && continue

        FROM_BLOCK=$(echo "$FILENAME" | sed 's/blocks_\([0-9]*\)_to_.*/\1/')
        TO_BLOCK=$(echo "$FILENAME" | sed 's/blocks_[0-9]*_to_\([0-9]*\)\.jsonl\.gz/\1/')

        # Use local file size if available, otherwise mark as unknown
        LOCAL_PATH="${FIX_ARCHIVE_DIR}/${FILENAME}"
        if [ -f "$LOCAL_PATH" ]; then
            FILE_SIZE=$(du -h "$LOCAL_PATH" | cut -f1)
        else
            FILE_SIZE="unknown"
        fi

        echo "${FILENAME},${FROM_BLOCK},${TO_BLOCK},${VAULT_PREFIX}/${FILENAME},${FILE_SIZE},${TIMESTAMP}" >> "$FIX_MANIFEST"
        echo "$FILENAME" >> "$FIX_UPLOADED"

        echo "  Found: ${FILENAME} [blocks ${FROM_BLOCK}-${TO_BLOCK}]"
    done

    echo ""
    echo "Manifest rebuilt: ${FIX_MANIFEST}"
    echo "Tracker rebuilt:  ${FIX_UPLOADED}"
    echo ""
    column -t -s',' "$FIX_MANIFEST" 2>/dev/null || cat "$FIX_MANIFEST"
    exit 0
fi

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
ARCHIVE_DIR="${1:-${ARCHIVE_DIR:-./sparkdream-archives}}"
MANIFEST_FILE="${MANIFEST_FILE:-${ARCHIVE_DIR}/jackal-manifest.csv}"
UPLOADED_FILE="${UPLOADED_FILE:-${ARCHIVE_DIR}/.jackal-uploaded}"

# ---------------------------------------------------------------------------
# Auto-detect mode from available credentials
# ---------------------------------------------------------------------------
if [ -z "$JACKAL_MODE" ]; then
    if [ -n "$JACKAL_MNEMONIC" ]; then
        JACKAL_MODE="vault"
    elif [ -n "$JACKAL_API_KEY" ]; then
        JACKAL_MODE="pin"
    else
        echo "ERROR: Set JACKAL_MNEMONIC (vault mode) or JACKAL_API_KEY (pin mode)." >&2
        echo "" >&2
        echo "  Vault (on-chain storage, one-time purchase):" >&2
        echo "    export JACKAL_MNEMONIC=\"your 24 word mnemonic ...\"" >&2
        echo "" >&2
        echo "  Pin (hosted IPFS, monthly billing):" >&2
        echo "    export JACKAL_API_KEY=\"your-api-key\"" >&2
        exit 1
    fi
fi

# ---------------------------------------------------------------------------
# Common preflight checks
# ---------------------------------------------------------------------------
for cmd in curl jq; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "ERROR: '$cmd' is required but not installed." >&2
        exit 1
    fi
done

if [ ! -d "$ARCHIVE_DIR" ]; then
    echo "ERROR: Archive directory not found: $ARCHIVE_DIR" >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# Mode-specific preflight checks
# ---------------------------------------------------------------------------
preflight_vault() {
    export JACKAL_RPC="${JACKAL_RPC:-https://rpc.jackalprotocol.com}"
    export JACKAL_REST="${JACKAL_REST:-https://api.jackalprotocol.com}"

    if ! command -v node >/dev/null 2>&1; then
        echo "ERROR: Node.js is required but not installed (need v20+)." >&2
        echo "Install from: https://nodejs.org/" >&2
        exit 1
    fi

    NODE_VERSION=$(node -v | sed 's/v//' | cut -d. -f1)
    if [ "$NODE_VERSION" -lt 20 ]; then
        echo "ERROR: Node.js 20+ required (found v${NODE_VERSION})." >&2
        exit 1
    fi

    if [ -z "$JACKAL_MNEMONIC" ]; then
        echo "ERROR: JACKAL_MNEMONIC environment variable is required for vault mode." >&2
        echo "  export JACKAL_MNEMONIC=\"your 24 word mnemonic ...\"" >&2
        exit 1
    fi

    # Resolve global node_modules so globally-installed packages are importable
    export NODE_PATH="${NODE_PATH:+${NODE_PATH}:}$(npm root -g)"

    if ! node -e "require('@jackallabs/jackal.js')" 2>/dev/null; then
        echo "ERROR: @jackallabs/jackal.js is not installed." >&2
        echo "  npm install -g @jackallabs/jackal.js" >&2
        exit 1
    fi

    export JACKAL_FOLDER="${JACKAL_FOLDER:-sparkdream-archives}"

    echo "Mode: vault (on-chain via jackal.js)"
    echo "RPC:  ${JACKAL_RPC}"
    if [ "$JACKAL_FOLDER" != "Home" ]; then
        echo "Folder: Home/${JACKAL_FOLDER}"
    else
        echo "Folder: Home (root)"
    fi
    echo ""
}

preflight_pin() {
    export JACKAL_API_URL="${JACKAL_API_URL:-https://pinapi.jackalprotocol.com}"

    if [ -z "$JACKAL_API_KEY" ]; then
        echo "ERROR: JACKAL_API_KEY environment variable is required for pin mode." >&2
        echo "  Generate one at: https://pin.jackalprotocol.com/keys" >&2
        echo "  export JACKAL_API_KEY=\"your-api-key\"" >&2
        exit 1
    fi

    echo "Mode: pin (Jackal Pin API)"
    echo "Verifying API key..."
    TEST_RESPONSE=$(curl -sf -w "\n%{http_code}" \
        -H "Authorization: Bearer ${JACKAL_API_KEY}" \
        "${JACKAL_API_URL}/test" 2>&1) || {
        echo "ERROR: Jackal Pin API is not reachable at ${JACKAL_API_URL}" >&2
        exit 1
    }

    HTTP_CODE=$(echo "$TEST_RESPONSE" | tail -1)
    if [ "$HTTP_CODE" = "401" ]; then
        echo "ERROR: Invalid API key (401 Unauthorized)." >&2
        echo "  Generate a new key at: https://pin.jackalprotocol.com/keys" >&2
        exit 1
    elif [ "$HTTP_CODE" != "200" ]; then
        echo "ERROR: API test returned HTTP $HTTP_CODE" >&2
        echo "$TEST_RESPONSE" >&2
        exit 1
    fi
    echo "  API key is valid."
    echo ""
}

case "$JACKAL_MODE" in
    vault) preflight_vault ;;
    pin)   preflight_pin ;;
    *)
        echo "ERROR: JACKAL_MODE must be 'vault' or 'pin' (got: '$JACKAL_MODE')" >&2
        exit 1
        ;;
esac

# ---------------------------------------------------------------------------
# Initialize manifest and tracker
# ---------------------------------------------------------------------------
if [ "$JACKAL_MODE" = "vault" ]; then
    MANIFEST_HEADER="file,from_block,to_block,jackal_path,cid,merkle,file_size,uploaded_at"
else
    MANIFEST_HEADER="file,from_block,to_block,cid,file_size,uploaded_at"
fi
if [ ! -f "$MANIFEST_FILE" ] || ! head -1 "$MANIFEST_FILE" | grep -q "^file,"; then
    # Prepend header if file is missing or has no header
    if [ -f "$MANIFEST_FILE" ] && [ -s "$MANIFEST_FILE" ]; then
        sed -i "1i\\${MANIFEST_HEADER}" "$MANIFEST_FILE"
        echo "Added header to existing manifest: $MANIFEST_FILE"
    else
        echo "$MANIFEST_HEADER" > "$MANIFEST_FILE"
        echo "Created manifest: $MANIFEST_FILE"
    fi
fi

touch "$UPLOADED_FILE"

# ---------------------------------------------------------------------------
# Collect files to upload
# ---------------------------------------------------------------------------
UPLOAD_COUNT=0
SKIP_COUNT=0
FAIL_COUNT=0
FILES_TO_UPLOAD=""

for ARCHIVE_FILE in $(ls "${ARCHIVE_DIR}"/blocks_*.jsonl.gz 2>/dev/null | sort -t_ -k2 -n); do
    FILENAME=$(basename "$ARCHIVE_FILE")

    if grep -qF "$FILENAME" "$UPLOADED_FILE" 2>/dev/null; then
        SKIP_COUNT=$(( SKIP_COUNT + 1 ))
        continue
    fi

    FROM_BLOCK=$(echo "$FILENAME" | sed 's/blocks_\([0-9]*\)_to_.*/\1/')
    TO_BLOCK=$(echo "$FILENAME" | sed 's/blocks_[0-9]*_to_\([0-9]*\)\.jsonl\.gz/\1/')
    FILE_SIZE=$(du -h "$ARCHIVE_FILE" | cut -f1)

    echo "Queued: $FILENAME ($FILE_SIZE) [blocks ${FROM_BLOCK}-${TO_BLOCK}]"

    if [ "${DRY_RUN}" = "true" ]; then
        echo "  [DRY RUN] Would upload $FILENAME"
        continue
    fi

    FILES_TO_UPLOAD="${FILES_TO_UPLOAD}${ARCHIVE_FILE}|${FILENAME}|${FROM_BLOCK}|${TO_BLOCK}|${FILE_SIZE}
"
done

if [ "${DRY_RUN}" = "true" ]; then
    echo ""
    echo "Dry run complete. No files uploaded."
    exit 0
fi

FILES_TO_UPLOAD=$(echo "$FILES_TO_UPLOAD" | sed '/^$/d')

if [ -z "$FILES_TO_UPLOAD" ]; then
    echo ""
    echo "No new files to upload (skipped: $SKIP_COUNT)."
    exit 0
fi

# ===========================================================================
# VAULT MODE: Upload via jackal.js SDK
# ===========================================================================
upload_vault() {
    echo ""
    echo "Connecting to Jackal (${JACKAL_RPC})..."

    UPLOAD_RESULT=$(echo "$FILES_TO_UPLOAD" | node -e "
const { readFileSync } = require('fs');
const { readFile } = require('fs/promises');
const { basename } = require('path');

// Redirect all console output to stderr so SDK debug noise does not
// mix with our JSON result lines on stdout.
const _stderr = process.stderr;
['log','warn','dir','error','info','debug'].forEach(m => {
    const orig = console[m];
    console[m] = (...args) => { _stderr.write(require('util').format(...args) + '\n'); };
});

const mnemonic = process.env.JACKAL_MNEMONIC;
const rpc = process.env.JACKAL_RPC;
const rest = process.env.JACKAL_REST;
const folder = process.env.JACKAL_FOLDER || 'sparkdream-archives';

const input = readFileSync(0, 'utf-8').trim();
if (!input) process.exit(0);

const files = input.split('\n').map(line => {
    const [path, name, from, to, size] = line.split('|');
    return { path, name, from, to, size };
});

(async () => {
    const { ClientHandler } = require('@jackallabs/jackal.js');

    const client = await ClientHandler.connect({
        selectedWallet: 'mnemonic',
        mnemonic,
        chainId: 'jackal-1',
        endpoint: rpc,
        chainConfig: {
            chainId: 'jackal-1',
            chainName: 'Jackal Mainnet',
            rpc,
            rest,
            bip44: {
                coinType: 118,
            },
            stakeCurrency: {
                coinDenom: 'JKL',
                coinMinimalDenom: 'ujkl',
                coinDecimals: 6,
            },
            bech32Config: {
                bech32PrefixAccAddr: 'jkl',
                bech32PrefixAccPub: 'jklpub',
                bech32PrefixValAddr: 'jklvaloper',
                bech32PrefixValPub: 'jklvaloperpub',
                bech32PrefixConsAddr: 'jklvalcons',
                bech32PrefixConsPub: 'jklvalconspub',
            },
            currencies: [
                {
                    coinDenom: 'JKL',
                    coinMinimalDenom: 'ujkl',
                    coinDecimals: 6,
                },
            ],
            feeCurrencies: [
                {
                    coinDenom: 'JKL',
                    coinMinimalDenom: 'ujkl',
                    coinDecimals: 6,
                    gasPriceStep: {
                        low: 0.002,
                        average: 0.002,
                        high: 0.02,
                    },
                },
            ],
            features: [],
        },
    });

    const storage = await client.createStorageHandler();

    // Load mainnet storage providers (from jackal-dashboard)
    await storage.loadProviderPool();
    await storage.upgradeSigner();
    await storage.initStorage();
    // Load root directory first
    await storage.loadDirectory({ path: 'Home' });
    console.log('Loaded home directory');

    // If a subfolder is requested, create it (if needed) and navigate into it
    if (folder !== 'Home') {
        const existing = storage.listChildFolders();
        if (!existing.includes(folder)) {
            // Creating a folder is an on-chain tx. The cosmjs broadcastTx has a
            // hardcoded 60s timeout that cannot be overridden via the Jackal SDK.
            // The tx is usually submitted fine but confirmation takes longer, so
            // we catch the timeout, wait for the chain to process it, then retry
            // the directory load.
            try {
                await storage.createFolders({ names: [folder], broadcastOptions: { monitorTimeout: 300 } });
            } catch (e) {
                const isTimeout = e?.txId || (e?.message || '').includes('TimeoutError') || (e?.errorText || '').includes('Timeout');
                if (!isTimeout) throw e;
                console.log('Folder tx submitted, waiting for on-chain confirmation...');
                // Poll until the folder appears (tx was submitted, just not confirmed in time)
                for (let attempt = 0; attempt < 30; attempt++) {
                    await new Promise(r => setTimeout(r, 10000));
                    await storage.loadDirectory({ path: 'Home', refresh: true });
                    if (storage.listChildFolders().includes(folder)) break;
                    console.log('  waiting... (' + (attempt + 1) * 10 + 's)');
                }
            }
            console.log('Created folder: ' + folder);
            // Reload Home to refresh the SDK's internal path cache after creation
            await storage.loadDirectory({ path: 'Home', refresh: true });
        } else {
            console.log('Folder exists: ' + folder);
        }
        await storage.loadDirectory({ path: 'Home/' + folder });
        console.log('Loaded subfolder: Home/' + folder);
    }

    // Handle cosmjs errors that bypass the normal promise chain.
    process.on('unhandledRejection', (err) => {
        const msg = err?.message || String(err);
        if (err?.txId || msg.includes('TimeoutError') || msg.includes('was submitted but was not yet found')) {
            console.log('Tx confirmation timed out — will verify in pass 2.');
        } else if (msg.includes('account sequence mismatch')) {
            console.error('');
            console.error('ERROR: Account sequence mismatch — the chain has a different');
            console.error('transaction count than the SDK expects. This usually happens');
            console.error('when a previous upload timed out mid-transaction.');
            console.error('');
            console.error('To fix, wait 2 minutes for pending transactions to settle,');
            console.error('then run clean twice to reset the SDK state:');
            console.error('');
            console.error('  sleep 120');
            console.error('  ./jackal-upload.sh clean <archive_dir>');
            console.error('  ./jackal-upload.sh clean <archive_dir>');
            console.error('  ./jackal-upload.sh <archive_dir>');
            console.error('');
            process.exit(1);
        } else {
            console.error('Unhandled rejection: ' + msg);
            process.exit(1);
        }
    });

    const prefix = folder === 'Home' ? 'Home' : 'Home/' + folder;

    // ---- PASS 1: Queue and upload all files (don't wait for confirmation) ----
    console.log('');
    console.log('=== Pass 1: Uploading files to providers ===');

    // Collect CIDs by intercepting the SDK's console output.
    // The SDK logs progress objects like: progress: 100 { merkle: '...', cid: '...' }
    // statusCallback is not a real SDK option — CIDs only appear in console output.
    const cidMap = {};  // merkle -> cid
    const origStderrWrite = _stderr.write.bind(_stderr);
    _stderr.write = (chunk, ...args) => {
        const str = typeof chunk === 'string' ? chunk : chunk.toString();
        // Match CID from SDK progress logs
        const cidMatch = str.match(/cid:\s*'(baf[a-z0-9]+)'/);
        const merkleMatch = str.match(/merkle:\s*'([A-Za-z0-9+/=]+)'/);
        const progressMatch = str.match(/progress:\s*100/);
        if (cidMatch && merkleMatch && progressMatch) {
            cidMap[merkleMatch[1]] = cidMatch[1];
        }
        return origStderrWrite(chunk, ...args);
    };

    const fileObjects = await Promise.all(files.map(async (f) => {
        const data = await readFile(f.path);
        return new File([data], f.name, { type: 'application/gzip' });
    }));

    await storage.queuePublic(fileObjects);
    console.log('Queued ' + fileObjects.length + ' file(s)');

    // Upload all files to providers and submit the on-chain tx.
    // The SDK hangs on its WebSocket monitor after the tx is submitted,
    // so we use a safety timeout to move on to pass 2 verification.
    const UPLOAD_TIMEOUT_MS = 180000;  // 3 min safety net
    await Promise.race([
        storage.processAllQueues({ monitorTimeout: 60 }).catch(() => {}),
        new Promise((resolve) => setTimeout(() => {
            console.log('Pass 1 complete (safety timeout) — proceeding to verification.');
            resolve();
        }, UPLOAD_TIMEOUT_MS)),
    ]);

    // ---- PASS 2: Poll vault for each file and capture CIDs ----
    console.log('');
    console.log('=== Pass 2: Verifying uploads and capturing CIDs ===');

    // Wait for chain to settle (tx confirmation)
    console.log('Waiting 30s for chain to process transactions...');
    await new Promise(r => setTimeout(r, 30000));

    // Reload the directory to see newly uploaded files
    await storage.loadDirectory({ path: prefix, refresh: true });
    const remoteFiles = storage.listChildFiles();
    console.log('Remote files: ' + remoteFiles.join(', '));

    // If statusCallback didn't capture any CIDs, fetch the CID map
    // from a single provider as fallback (download once, reuse for all files)
    let providerCidMap = null;
    if (Object.keys(cidMap).length === 0) {
        console.log('No CIDs from statusCallback — fetching from provider...');
        const provRes = await fetch(rest + '/jackal/canine-chain/storage/active_providers');
        const provAddrs = (await provRes.json()).providers.map(p => p.address);
        for (const addr of provAddrs.slice(0, 3)) {
            try {
                const r = await fetch(rest + '/jackal/canine-chain/storage/providers/' + addr);
                const ip = (await r.json()).provider.ip;
                if (!ip) continue;
                const mapRes = await fetch(ip + '/ipfs/cid_map', { signal: AbortSignal.timeout(15000) });
                providerCidMap = (await mapRes.json()).cid_map || {};
                console.log('  Loaded CID map from ' + ip + ' (' + Object.keys(providerCidMap).length + ' entries)');
                break;
            } catch {}
        }
    }

    for (const f of files) {
        let cid = '';
        let merkleHex = '';

        try {
            const meta = await storage.getFileParticulars(prefix + '/' + f.name);
            const merkle = meta?.merkle;
            if (merkle) {
                // The statusCallback stores merkle as-is from the SDK (base64 string).
                // getFileParticulars returns merkle as raw bytes (Uint8Array) or string.
                // Try all possible encodings to match.
                const merkleB64 = typeof merkle === 'string' ? merkle : Buffer.from(merkle).toString('base64');
                merkleHex = typeof merkle === 'string' ? Buffer.from(merkle, 'base64').toString('hex') : Buffer.from(merkle).toString('hex');

                // Try exact match, then base64, then hex
                cid = cidMap[merkleB64] || cidMap[merkleHex] || '';

                // Also try matching against all cidMap keys (SDK may pad/format differently)
                if (!cid) {
                    for (const [k, v] of Object.entries(cidMap)) {
                        if (k === merkleB64 || k === merkleHex ||
                            Buffer.from(k, 'base64').toString('hex') === merkleHex) {
                            cid = v;
                            break;
                        }
                    }
                }

                // Fallback: look up in provider CID map by merkle hex
                if (!cid && providerCidMap) {
                    // Provider CID map keys are hex-encoded — try matching our merkle hex
                    cid = providerCidMap[merkleHex] || '';
                    if (!cid) {
                        // Keys may have path prefixes, search for suffix match
                        for (const [k, v] of Object.entries(providerCidMap)) {
                            try {
                                const decoded = Buffer.from(k, 'hex').toString('utf-8');
                                if (decoded.includes(merkleHex)) { cid = v; break; }
                            } catch {}
                            if (k.includes(merkleHex)) { cid = v; break; }
                        }
                    }
                }
            }
        } catch (err) {
            console.log('  Could not get metadata for ' + f.name + ': ' + err.message);
        }

        const uploaded = remoteFiles.includes(f.name);
        if (uploaded) {
            console.log('  OK: ' + f.name + (cid ? ' (CID: ' + cid + ')' : '') + (merkleHex ? ' (merkle: ' + merkleHex.slice(0, 16) + '...)' : ''));
            process.stdout.write(JSON.stringify({ ok: true, file: f.name, from: f.from, to: f.to, size: f.size, id: prefix + '/' + f.name, cid, merkle: merkleHex || '' }) + '\n');
        } else {
            console.log('  MISSING: ' + f.name + ' — not found in vault');
            process.stdout.write(JSON.stringify({ ok: false, file: f.name, error: 'File not found in vault after upload' }) + '\n');
        }
    }
    process.exit(0);
})().catch(err => {
    process.stderr.write('Fatal: ' + err.message + '\n');
    process.exit(1);
});
") || {
        echo "ERROR: Jackal upload process failed." >&2
        echo "$UPLOAD_RESULT" >&2
        exit 1
    }

    echo "$UPLOAD_RESULT"
}

# ===========================================================================
# PIN MODE: Upload via Jackal Pin REST API
# ===========================================================================
upload_pin() {
    echo ""
    echo "Uploading to Jackal Pin..."

    local RESULTS=""
    while IFS='|' read -r ARCHIVE_FILE FILENAME FROM_BLOCK TO_BLOCK FILE_SIZE; do
        UPLOAD_OUTPUT=$(curl -sf \
            -H "Authorization: Bearer ${JACKAL_API_KEY}" \
            -F "file=@${ARCHIVE_FILE}" \
            "${JACKAL_API_URL}/files" 2>&1) || {
            RESULTS="${RESULTS}$(printf '{"ok":false,"file":"%s","error":"upload request failed"}' "$FILENAME")\n"
            continue
        }

        CID=$(echo "$UPLOAD_OUTPUT" | jq -r '.cid // .CID // .Hash // empty' 2>/dev/null)

        if [ -z "$CID" ]; then
            RESULTS="${RESULTS}$(printf '{"ok":false,"file":"%s","error":"no CID in response"}' "$FILENAME")\n"
            continue
        fi

        RESULTS="${RESULTS}$(printf '{"ok":true,"file":"%s","from":"%s","to":"%s","size":"%s","id":"%s"}' \
            "$FILENAME" "$FROM_BLOCK" "$TO_BLOCK" "$FILE_SIZE" "$CID")\n"
    done <<< "$FILES_TO_UPLOAD"

    echo -e "$RESULTS"
}

# ---------------------------------------------------------------------------
# Run the upload
# ---------------------------------------------------------------------------
if [ "$JACKAL_MODE" = "vault" ]; then
    UPLOAD_RESULT=$(upload_vault)
else
    UPLOAD_RESULT=$(upload_pin)
fi

# ---------------------------------------------------------------------------
# Process results
# ---------------------------------------------------------------------------
echo ""
while IFS= read -r line; do
    [ -z "$line" ] && continue

    # Skip non-JSON lines (connection logs, etc)
    if ! echo "$line" | jq -e . >/dev/null 2>&1; then
        echo "  $line"
        continue
    fi

    OK=$(echo "$line" | jq -r '.ok')
    FILENAME=$(echo "$line" | jq -r '.file')

    if [ "$OK" = "true" ]; then
        ID=$(echo "$line" | jq -r '.id')
        CID=$(echo "$line" | jq -r '.cid // empty')
        MERKLE=$(echo "$line" | jq -r '.merkle // empty')
        FROM_BLOCK=$(echo "$line" | jq -r '.from')
        TO_BLOCK=$(echo "$line" | jq -r '.to')
        FILE_SIZE=$(echo "$line" | jq -r '.size')
        TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

        echo "Uploaded: $FILENAME -> $ID"
        [ -n "$CID" ] && echo "  CID: $CID"
        [ -n "$MERKLE" ] && echo "  Merkle: ${MERKLE:0:16}..."

        if [ "$JACKAL_MODE" = "vault" ]; then
            echo "${FILENAME},${FROM_BLOCK},${TO_BLOCK},${ID},${CID},${MERKLE},${FILE_SIZE},${TIMESTAMP}" >> "$MANIFEST_FILE"
        else
            echo "${FILENAME},${FROM_BLOCK},${TO_BLOCK},${CID},${FILE_SIZE},${TIMESTAMP}" >> "$MANIFEST_FILE"
        fi
        echo "$FILENAME" >> "$UPLOADED_FILE"

        UPLOAD_COUNT=$(( UPLOAD_COUNT + 1 ))
    else
        ERROR=$(echo "$line" | jq -r '.error')
        echo "FAILED: $FILENAME - $ERROR" >&2
        FAIL_COUNT=$(( FAIL_COUNT + 1 ))
    fi
done <<< "$UPLOAD_RESULT"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "========================================"
echo "Jackal upload complete (${JACKAL_MODE} mode)"
echo "  New uploads:  $UPLOAD_COUNT"
echo "  Skipped:      $SKIP_COUNT"
echo "  Failed:       $FAIL_COUNT"
echo "  Manifest:     $MANIFEST_FILE"
echo "========================================"

if [ "$UPLOAD_COUNT" -gt 0 ] || [ "$SKIP_COUNT" -gt 0 ]; then
    echo ""
    echo "Manifest contents:"
    echo ""
    column -t -s',' "$MANIFEST_FILE" 2>/dev/null || cat "$MANIFEST_FILE"
fi

if [ "$UPLOAD_COUNT" -gt 0 ]; then
    echo ""
    if [ "$JACKAL_MODE" = "pin" ]; then
        echo "Access archives via IPFS:"
        echo "  https://ipfs.io/ipfs/<CID>"
        echo ""
        echo "Manage your plan at: https://pin.jackalprotocol.com"
    else
        echo "Archives stored on-chain via Jackal vault."
        echo "Manage your storage at: https://vault.jackalprotocol.com"
    fi
fi
