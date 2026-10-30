# CLAUDE.md — pack2dockerfile 项目宪法

## 必读上下文

- `docs/元宝讨论.md` — 项目缘起与定位（2026-10-30 对话纪要，权威来源）
- `docs/项目启动手册.md` — 从 0 到开工的固定流程，本项目按此推进
- `docs/规格.md` — 规格（宪法票）：通过线、里程碑、风险底线；修订必须记录理由
- `docs/.scratch/issues/` — 票清单（票01–06），依赖链 01 → 02 → 03 → 04/05/06，无环
- `docs/` 全部为**本地-only**（独立 git 仓库，已忽略于公开仓库；稍后推 NAS Gitea）。公开仓库只有代码与英文文档
- 失联兜底：上述文件缺失或规格被移走时，停下问用户，不要凭记忆继续

## 方法纪律

- **可证伪先行**：没有 ground truth 不写引擎；通过线先于实现，修订必须记录理由
- **结论落档**：共识/增补带日期；每票完成落 `docs/实验记录-*.md`（可复现命令 + 数据 + 判定：过线/不过线/证据不足）
- **关键取舍由用户拍板**（升格、换路线），不在代码里自作主张

## 工作流约定

- **项目定位**：Buildpacks → Dockerfile 编译器（反汇编器）。Dockerfile 是产物，不是 source of truth
- **命名**：仓库 `pack2dockerfile` / CLI `p2df` / 人类 alias `p2`
- **MVP 范围**（讨论稿，待收敛定案）：Paketo Node/Java/Python/Go → 模板化 Dockerfile；输入 `pack inspect-image` + SBOM，不真反编译层
- **Tracker**：`docs/.scratch/issues/`（随 docs 一起本地-only），一票一文件，字段 What to build / Why / Status / Blocked by / 验收项（checkbox 可核）
- **代码目录**：待收敛后确定（讨论稿倾向 Go + cobra，未定案）
- **提交**：中文提交信息，`feat(scope): ...（票NN·任务N.N）`；TDD 红绿；尾注 `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
