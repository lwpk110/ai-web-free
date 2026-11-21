#!/usr/bin/env bash
set -euo pipefail

# ==============================================================================
# setup_github_branch_protection.sh
# 使用 GitHub CLI (gh) 一键为 main 分支开启保护规则：
#   1. 禁止直接 push 到 main 分支（含管理员 enforce_admins=true）
#   2. 强制要求通过 Pull Request 合并
#   3. 强制要求 GitHub Actions CI (test-and-build) 状态检查通过后才允许合并
# ==============================================================================

BRANCH="${1:-main}"

if ! command -v gh >/dev/null 2>&1; then
  echo "错误: 未检测到 GitHub CLI (gh)，请先安装 gh 并执行 'gh auth login'" >&2
  exit 1
fi

REPO="$(gh repo view --json nameWithOwner -q .nameWithOwner)"
if [ -z "${REPO}" ]; then
  echo "错误: 无法识别当前 GitHub 仓库，请在仓库根目录下运行" >&2
  exit 1
fi

echo "正在为仓库 ${REPO} 的 [${BRANCH}] 分支应用强制 PR + CI 分支保护规则..."

MSYS_NO_PATHCONV=1 gh api \
  --method PUT \
  -H "Accept: application/vnd.github+json" \
  "repos/${REPO}/branches/${BRANCH}/protection" \
  --input - <<'EOF'
{
  "required_status_checks": {
    "strict": true,
    "contexts": [
      "test-and-build"
    ]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": false,
    "required_approving_review_count": 0
  },
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "block_creations": false,
  "required_conversation_resolution": true
}
EOF

echo "✅ 分支保护已生效：[${BRANCH}] 分支已禁止直接提交，所有代码必须通过 PR 且通过 CI (test-and-build) 后方可合并！"
