[English](README.md) | **简体中文**

# TaskFlow

TaskFlow 是一款用于个人任务管理和项目协作的 Web 应用，支持今日计划、共享项目、循环任务和 Discord 频道提醒。你可以直接使用在线服务，也可以自行部署。

[使用 TaskFlow](https://taskflow.hancongzhang.com) · [本地运行](#本地运行) · [反馈问题](https://github.com/pluto-han/TaskFlow/issues)

## 功能

**今日计划。** 在 Today 中创建和管理当天任务。每天午夜，前一天的任务会从今日列表移出，历史记录仍然保留，可以在 All tasks 中按计划日期查看。日期按荷兰时间（Europe/Amsterdam）划分，自动适配夏令时。

**项目与协作。** 将相关任务归入项目，设置优先级和截止时间，并按完成状态或优先级筛选。你可以邀请其他用户加入项目。个人任务保持私密；共享项目的成员可以编辑、完成和删除项目内的任务。

**循环任务。** 设置每天重复或每隔指定天数重复的任务，到期后会自动在 Today 中创建对应任务。你可以预览后续日期、调整计划或暂停循环，已经生成的任务和历史记录会保留。

## 开始使用

打开 [TaskFlow](https://taskflow.hancongzhang.com)，注册账号后即可在 Today 中添加任务。使用 Projects 管理相关任务，或在 Recurring tasks 中设置循环计划。

共享项目时，先请对方提供 Profile 中显示的账号 ID，再打开项目中的 Members & invitations，为该账号创建邀请并将链接分享给对方。

任务会保存到你的账号中。登录后，刷新页面或打开新标签页仍可继续使用，会话最长有效 24 小时。退出登录会使服务端会话失效。

## Discord 提醒

在 Profile 中绑定 Discord 频道，再为需要提醒的循环任务开启通知。到期任务创建后，机器人会向绑定频道发送提醒，并 @ 绑定的 Discord 用户。提醒不受任务完成状态影响，也不会持续催办。

绑定前，需要先将 TaskFlow 机器人安装到你的 Discord 服务器。在 Profile 中生成绑定码，然后到目标频道执行页面提供的 `/taskflow bind` 命令。你需要有管理该频道的权限，机器人需要有查看频道和发送消息的权限。提醒中的任务标题对该频道的所有可读成员可见。

## 本地运行

本地运行需要 Docker、Docker Compose v2、Python 3 和 OpenSSL。应用通过 Docker 构建，无需在宿主机另行安装 Go 或 Node.js。

```bash
git clone https://github.com/pluto-han/TaskFlow.git
cd TaskFlow
python3 scripts/init_local_stack.py
docker compose -f compose.production.yaml build
docker compose -f compose.production.yaml up -d --wait --wait-timeout 180
```

启动后打开 [https://localhost:8443](https://localhost:8443) 并注册账号。本地配置会生成测试证书；需要将 `.local/stack/ca.crt` 作为证书颁发机构导入测试浏览器，浏览器才会信任该证书。这套证书仅用于本地测试，不适用于公网部署。

初始化脚本仅需在首次安装时运行。请保留 `.local/stack` 目录，其中包含现有数据库所需的凭据。任务数据存储在 Docker 数据卷中，容器重启后仍会保留。

这套本地配置默认关闭循环任务自动生成、Discord 消息发送和 Redis 缓存。它们需要单独配置；如需启用，请查看 [Compose 配置](compose.production.yaml)和[开发说明](README_dev.md)。

## 反馈与开发

遇到问题或希望增加功能，可以[提交 Issue](https://github.com/pluto-han/TaskFlow/issues)。报告问题时，请说明复现步骤、预期行为和实际结果。也欢迎通过 Pull Request 参与改进。
