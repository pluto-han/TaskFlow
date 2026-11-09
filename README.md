**English** | [简体中文](README_zh-CN.md)

# TaskFlow

TaskFlow is a web application for personal task management and project collaboration. It supports daily planning, shared projects, recurring tasks, and Discord channel reminders. You can use the hosted application or run your own instance.

[Open TaskFlow](https://taskflow.hancongzhang.com) · [Run locally](#run-locally) · [Report an issue](https://github.com/pluto-han/TaskFlow/issues)

## Features

**Daily planning.** Create and manage tasks in Today. At midnight, tasks from the previous day leave the Today view and remain available in All tasks through the planned date filter. Daily planning follows the Europe/Amsterdam time zone, including daylight saving changes.

**Projects and collaboration.** Organize tasks into projects, assign priorities and due dates, and filter by status or priority. Invite other users to shared projects. Personal tasks remain private; project members can edit, complete, and delete tasks within shared projects.

**Recurring tasks.** Schedule tasks to repeat daily or at a chosen interval in days. New occurrences appear in Today on their scheduled dates. Preview upcoming dates, edit schedules, or pause recurring tasks while retaining existing task history.

## Get started

Visit [TaskFlow](https://taskflow.hancongzhang.com), create an account, and add your first task in Today. Use Projects to organize related tasks and Recurring tasks to configure repeating schedules.

To share a project, ask the other person for the account ID shown in their Profile. Open Members & invitations in your project, create an invitation for that account, and share the link with them.

Tasks are saved to your account. Currently, refreshing the page or opening a new tab requires signing in again.

## Discord reminders

Connect a Discord channel from your Profile and enable reminders for selected recurring tasks. When a scheduled task is created, the bot sends a reminder to the connected channel and mentions the bound Discord user. Delivery is independent of task completion, and the bot does not send follow-up reminders.

The TaskFlow bot must already be installed in your Discord server. Generate a connection code in Profile and use the supplied `/taskflow bind` command in the channel you want to connect. You need permission to manage that channel, and the bot needs permission to view it and send messages. Task titles in reminders are visible to everyone who can read the channel.

## Run locally

You can also run TaskFlow on your own computer. You will need Docker with Compose v2, Python 3, and OpenSSL. Docker builds the application, so you do not need to install Go or Node.js for this setup.

```bash
git clone https://github.com/pluto-han/TaskFlow.git
cd TaskFlow
python3 scripts/init_local_stack.py
docker compose -f compose.production.yaml build
docker compose -f compose.production.yaml up -d --wait --wait-timeout 180
```

Open [https://localhost:8443](https://localhost:8443) and create your account. The local setup generates a test certificate; your browser will not trust it until you import `.local/stack/ca.crt` as a certificate authority for local testing. These certificates are for local use, not a public server.

Run the initialization script only once. Keep `.local/stack` with your installation, as it contains the credentials needed by your existing database. Task data is stored in a Docker volume and survives container restarts.

Automatic recurring task generation, Discord delivery, and Redis caching are disabled by default in this local setup. Their settings are separate from starting the app; see the [Compose configuration](compose.production.yaml) and [developer notes](README_dev.md) if you want to configure your own instance.

## Feedback and development

For bug reports and feature requests, [open an issue](https://github.com/pluto-han/TaskFlow/issues). Include the steps to reproduce the problem, the expected behavior, and the actual result. Contributions through pull requests are welcome.
