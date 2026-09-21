# docs

本目录存放项目内部设计与实现文档，**不是**面向用户的使用说明（用户使用说明见仓库根目录的 `README.md`）。

## 目录结构

- `superpowers/specs/` — 由 AI 辅助开发工具（superpowers）按其约定输出到 `docs/superpowers/specs/` 的特性设计文档。
  - `2026-09-07-recharge-sidebar-design.md`：侧边栏新增「代充」菜单与页面的设计文档（classic / default 双栈落地方案）。
  - `2026-09-20-solar-hero-artword-design.md`：首页天体系统中央字标由 `heqiuyu` 换成单字母 `H` 的 v1/v2 设计（轨道同构 → 负形刻蚀）。
  - `2026-09-20-solar-glyph-v3-sunspot-design.md`：中央 `H` 字标 v3「太阳黑子」重设计（字体轮廓 + 全冷色 + 内敛强度），含 `--font-display` 字体名缺陷修复。
  - 该类文档带有 AIGC 标识与「内容由AI生成，仅供参考」尾注，属于**参考性设计稿**，可由对应需求的提交保留归档；文档描述的行为若与代码不一致，以代码为准。

## 维护约定

- 文档命名建议沿用 `<日期>-<主题>-design.md`，便于按时间检索。
- 新增设计文档时同步在本文件中登记一行说明。
