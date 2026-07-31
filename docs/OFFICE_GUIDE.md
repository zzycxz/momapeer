# 办公自动化使用指南

## 概述

MoMAPeer 提供完整的办公自动化功能，包括邮件管理、日历任务、PPT 生成和定时任务，帮助您高效处理日常办公事务。

## 邮件集成

### 配置邮箱

```toml
[cowork]
[[cowork.email_accounts]]
name = "工作邮箱"
smtp_host = "smtp.example.com"
smtp_port = 465
smtp_tls = true
imap_host = "imap.example.com"
imap_port = 993
username = "your@email.com"
password_env = "EMAIL_PASSWORD"
```

### 使用邮件功能

- **发送邮件**：使用 `email_send` 工具
- **读取邮件**：使用 `email_read` 工具
- **搜索邮件**：使用 `email_search` 工具

## 日历任务

### 创建定时任务

1. 打开「日历与任务」面板
2. 点击「新建任务」
3. 配置执行时间和提示词
4. 保存任务

### 任务类型

| 类型 | 说明 | 示例 |
|---|---|---|
| **定时提醒** | 在指定时间发送提醒 | 每天 9:00 提醒开晨会 |
| **定时执行** | 在指定时间执行任务 | 每周一生成周报 |
| **一次性任务** | 只执行一次 | 明天 15:00 发送邮件 |

### 配置

```toml
[cowork]
# 日历功能
calendar_enabled = true
```

## PPT 生成

### 使用 PPT 技能

1. 在对话中描述 PPT 需求
2. 选择 PPT 模板（可选）
3. AI 自动生成 PPT 文件

### PPT 模板

- **通用模板**：适用于大多数场景
- **自定义模板**：放置在 `.momapeer/skills/ppt-auto/templates/`

### 配置

```toml
[cowork]
# PPT 模式
ppt_mode = "svg"  # svg 或 wps
ppt_active_template = ""  # 模板 ID
```

## 最佳实践

1. **邮箱配置**：使用授权码而非密码
2. **定时任务**：设置合理的执行时间，避免冲突
3. **PPT 生成**：提供详细的需求描述，效果更好
4. **资源管理**：定期清理不需要的定时任务和邮件
