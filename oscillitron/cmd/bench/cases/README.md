<!-- CLAUDE GENERATED -->

# Bench case snapshots

Operator-supplied JSON snapshots of benchmark datasets. The bench
driver reads from disk only — it never pulls datasets from the
network at runtime. Reasons:

- Reproducibility: a committed snapshot ties a result to exact inputs.
- Offline-capable: runs work without network access.
- License clarity: most benchmark datasets are operator-licensed
  (HuggingFace gated, etc.); committing them mixes our license with
  theirs.

## GPQA Diamond

1. Authenticate with HuggingFace and accept the dataset terms
   (requires a HF account):

   ```sh
   huggingface-cli login
   ```

2. Download the dataset:

   ```sh
   huggingface-cli download Idavidrein/gpqa --repo-type dataset \
     --local-dir /tmp/gpqa
   ```

3. The CSV at `/tmp/gpqa/gpqa_diamond.csv` has columns
   `Question`, `Correct Answer`, `Incorrect Answer 1/2/3`, plus
   metadata. Convert to the JSON shape the loader expects:

   ```sh
   python3 - <<'PY' > gpqa_diamond.json
   import csv, json, sys
   out = []
   with open('/tmp/gpqa/gpqa_diamond.csv') as f:
       for i, row in enumerate(csv.DictReader(f)):
           out.append({
               'id': f'gpqa-diamond-{i+1:03d}',
               'question': row['Question'],
               'correct_answer': row['Correct Answer'],
               'incorrect_answers': [
                   row['Incorrect Answer 1'],
                   row['Incorrect Answer 2'],
                   row['Incorrect Answer 3'],
               ],
               'subdomain': row.get('Subdomain', ''),
           })
   print(json.dumps(out, indent=2))
   PY
   ```

4. The resulting `gpqa_diamond.json` lives in this directory and is
   read by `pkg/benchmark/loader/gpqa.Loader`. **Do not commit it** —
   the dataset is operator-licensed.

5. Run the bench:

   ```sh
   go run ./cmd/bench --benchmark gpqa \
     --cases cmd/bench/cases/gpqa_diamond.json
   ```

## Other benchmarks

Each benchmark gets its own loader under `pkg/benchmark/loader/...`
and its own README section here. MATH-500, AIME, MMLU-Pro, HLE, and
SWE-bench loaders are queued behind GPQA — when added they'll
document their own conversion recipes.
