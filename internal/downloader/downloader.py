#!/usr/bin/env python3
import os
import sys

SOURCE_TYPE = os.environ["SOURCE_TYPE"]          # huggingface | s3
TARGET_PATH = os.environ["TARGET_PATH"]          # /model
COMPLETE_FILE = os.path.join(TARGET_PATH, ".praesto-complete")

# Idempotenza: se il download è già stato fatto, esci subito
# Utile se il Job va in retry per un errore di rete a metà
if os.path.exists(COMPLETE_FILE):
    print(f"Model already downloaded at {TARGET_PATH}, skipping.")
    sys.exit(0)

os.makedirs(TARGET_PATH, exist_ok=True)

if SOURCE_TYPE == "huggingface":
    from huggingface_hub import snapshot_download

    repo_id   = os.environ["HF_REPO"]
    revision  = os.environ.get("HF_REVISION", "main")
    token     = os.environ.get("HF_TOKEN")       # None se non serve

    print(f"Downloading {repo_id}@{revision} from HuggingFace...")

    snapshot_download(
        repo_id=repo_id,
        revision=revision,
        local_dir=TARGET_PATH,
        token=token,
        ignore_patterns=["*.msgpack", "*.h5"],   # scarica solo safetensors + config
    )

elif SOURCE_TYPE == "s3":
    import boto3

    bucket = os.environ["S3_BUCKET"]
    prefix = os.environ["S3_PREFIX"]             # es. llama-3-8b/
    region = os.environ.get("S3_REGION", "us-east-1")

    print(f"Downloading s3://{bucket}/{prefix} to {TARGET_PATH}...")

    s3 = boto3.client("s3", region_name=region)
    paginator = s3.get_paginator("list_objects_v2")

    for page in paginator.paginate(Bucket=bucket, Prefix=prefix):
        for obj in page.get("Contents", []):
            key = obj["Key"]
            relative = key[len(prefix):]
            if not relative:
                continue
            dest = os.path.join(TARGET_PATH, relative)
            os.makedirs(os.path.dirname(dest), exist_ok=True)
            print(f"  {key} → {dest}")
            s3.download_file(bucket, key, dest)

else:
    print(f"Unknown SOURCE_TYPE: {SOURCE_TYPE}", file=sys.stderr)
    sys.exit(1)

# Scrivi il file di completamento
with open(COMPLETE_FILE, "w") as f:
    import datetime
    f.write(datetime.datetime.utcnow().isoformat())

print("Download complete.")
sys.exit(0)