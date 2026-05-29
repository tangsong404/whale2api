# 原始流数据样本目录

该目录只保留**上游真实 SSE 原始流**，用于本地回放、字段分析和回归测试。

## 样本分类

该目录下的样本分成两类：

- canonical 默认样本：由 [`manifest.json`](./manifest.json) 的 `default_samples` 指定，默认回放工具优先跑这组稳定样本
- 扩展样本：保留真实问题或特定协议行为，用于排障、字段分析和定向回归，不一定默认纳入全量回放

当前目录里除了 canonical 样本，还包含例如：

- `markdown-format-example-20260405`
- `markdown-format-example-20260405-spacefix`
- `continue-thinking-snapshot-replay-20260405`

其中 `continue-thinking-snapshot-replay-20260405` 是一个多轮样本，覆盖 `completion + continue` 的原始 SSE 重放场景，用于验证接续思考去重。

如果要看默认固定回放集，以 [`manifest.json`](./manifest.json) 为准，而不是按目录数量人工判断。
更完整的协议级行为结构说明见 [docs/DeepSeekSSE行为结构说明-2026-04-05.md](../../docs/DeepSeekSSE行为结构说明-2026-04-05.md)。

## 自动采集接口

本地启动服务后，可以直接调用专用接口自动落盘一份 raw-only 样本：

```bash
POST /admin/dev/raw-samples/capture
```

这个接口会：

- 接收一个普通的 OpenAI chat completions 请求体
- 走项目内同一条处理链
- 自动保存请求元信息 `meta.json`
- 自动保存上游原始流 `upstream.stream.sse`

采集接口的响应体仍然是项目当次的实际输出，但它不会再写入样本目录。这样样本树始终只保留原始流，后续回放时再按需本地生成派生结果。

如果问题已经在当前进程的内存抓包里复现过，也可以先查再存：

```bash
GET /admin/dev/raw-samples/query?q=关键词&limit=20
POST /admin/dev/raw-samples/save
```

这条链路适合把“刚刚发生的一次真实问题”快速转成可回放样本，而不用重新触发请求。

## 目录规范

每个样本一个子目录，且只保留下面两类文件：

- `meta.json`：样本元信息（问题、模型、采集时间、备注）
- `upstream.stream.sse`：完整原始 SSE 文本（`event:` / `data:` 行）

`meta.json` 的关键字段通常包括：

- `sample_id`
- `captured_at_utc`
- `source`
- `request`
- `capture`

对于多轮样本，`capture.rounds` 会记录每一轮上游请求，例如首轮 `deepseek_completion` 和后续 `deepseek_continue`。

## 回放与对比

回放工具会读取 `upstream.stream.sse`，在本地自动生成当前解析结果，并把派生结果写到 `artifacts/raw-stream-sim/<run-id>/<sample-id>/`，例如：

- `replay.output.txt`：本次回放生成的最终可见文本
- `report.json`：本次回放的结构化报告，包含事件数、chunk 数、终态、引用泄露检查等信息

运行全部 canonical 样本：

```bash
./tests/scripts/run-raw-stream-sim.sh
```

运行**全部样本目录**（不只 manifest 默认样本），并逐个打印 token 对齐结果：

```bash
for d in tests/raw_stream_samples/*; do
  [ -d "$d" ] || continue
  sid="$(basename "$d")"
  [ -f "$d/upstream.stream.sse" ] || continue
  node tests/tools/deepseek-sse-simulator.mjs --samples-root tests/raw_stream_samples --sample-id "$sid"
done
```

回放输出会显示 `tokens=<parsed>/<expected>`；默认只记录 token 差异，不因 token 不一致失败。如需把 token 差异作为失败条件，给模拟器增加 `--fail-on-token-mismatch`。`report.json` 中也会包含：

- `raw_expected_output_tokens`
- `raw_parsed_output_tokens`
- `raw_token_mismatch`

运行单个样本并和已有基线比对：

```bash
./tests/scripts/compare-raw-stream-sample.sh markdown-format-example-20260405-spacefix
```

如果你已经有历史基线目录，也可以把它作为第二个参数传进去，脚本会对比当前回放结果和基线输出。

## 扩展方式

1. 抓取一次真实请求。
2. 直接调用 `/admin/dev/raw-samples/capture`，或者先用 `/admin/dev/raw-samples/query` + `/admin/dev/raw-samples/save` 从内存抓包落盘；也可以手工新建 `<sample-id>/` 目录并放入 `meta.json` + `upstream.stream.sse`。
3. 运行回放工具或对比脚本，生成本地派生结果并检查是否回归。

> 注意：样本可能包含搜索结果正文与引用信息，请勿放入敏感账号/密钥。
