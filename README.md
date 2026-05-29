# Whale2API

DeepSeek 反代，支持 256K 上下文的 `deepseek-v4-flash`

## 致谢与声明

本项目在 **[ds2api](https://github.com/CJackHwang/ds2api)**（作者 [CJackHwang](https://github.com/CJackHwang)）基础上二次开发并重命名为 Whale2API

感谢上游作者及贡献者的开源工作。

CJackHwang佬的作品我个人使用过很长一段时间，为我减轻了许多经济上的负担，中转太多导致官方出手很是可惜

本项目重新提供更稳定版本的开源。256K的 `deepseek-v4-flash` 效果很不好，还为了防封禁做了很多负面调整，希望自用而非盈利

## 快速开始

### Docker

```bash
copy .env.example .env
# 编辑 .env：设置 POOL_UI_ADMIN_TOKEN
docker compose up -d --build
```

### 本地开发

```bash
go run ./cmd/whale2api   # :5001
go run ./cmd/poolui      # :5010
go test ./...
go run ./cmd/whale2api-tests # 集成测试
```

## 使用说明

| 地址 | 用途 |
|------|------|
| http://127.0.0.1:5103/v1/chat/completions | 网关 API |
| http://127.0.0.1:5010 | 号池 WebUI |

仅支持OpenAI Chat Completions兼容，高强度使用每天禁言2-3个号，建议50个号起用（批量注册参考我的仓库 `signup-god`）

导入csv格式: `email,password`

## 改了什么

1.去除提示词中所有和`DS2API`相关文本

2.将提示词全部转为中文，工具调用符号大改（经常导致出错）

3.不再使用 `deepseek-v4-pro` (没有文件上传=没有长历史)

4.限制上下文为 256K

5.增加了对`禁言`（不是`封禁`）机制的检测

## 参与贡献

欢迎通过 Pull Request 参与改进，包括但不限于：

- Bug 修复与测试补充
- 文档与使用说明完善
- 号池、网关稳定性与可观测性优化

提交 PR 前建议：

1. 在本地执行 `go test ./...` 确保通过
2. 保持改动聚焦，说明修改动机与验证方式
3. Fork 后从功能分支发起 PR，便于 review

如有较大改动，建议先开 Issue 简要讨论方案，避免重复劳动。
