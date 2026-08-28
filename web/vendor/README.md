# web/vendor — 第三方前端依赖（vendored）

## vis-network.min.js

vis-network@10.1.2 官方 standalone UMD（**435 KB**，含 license 头注释），
浏览器 global `vis`（`vis.Network` / `vis.DataSet`）。npm 包
`vis-network` 的 `dist/vis-network.min.js` 直接拷贝，无构建步骤。
Wiki 知识图谱第四套选型对照原型（`docs/prototypes/prototype-wiki-graph-vis.html`）用。

复现命令：

```bash
npm install vis-network@10.1.2
cp node_modules/vis-network/dist/vis-network.min.js web/vendor/vis-network.min.js
```

使用注意（踩坑记录）：

- **FA2（forceAtlas2Based）不定类目簇**：用"隐形枢纽"技巧诱导——每类目一个
  全透明 `chosen:false` 枢纽节点（参与物理）+ 成员→枢纽短弹簧（`length: 30`、
  透明色、不可选），孤立点也被拉进簇。`hidden: true` 会退出物理，必须用透明色。
- 无元素状态机，高亮/淡化用 DataSet `update()` 批量改 color/label。
- tooltip 用节点 `title`（HTML 字符串）+ CSS 覆盖 `.vis-tooltip` 容器样式。
- Canvas 2D 渲染，headless 截图无 WebGL 要求。

## g6.min.js

AntV G6 v5 的单文件 IIFE bundle（**1.4 MB**，含 @antv/g、@antv/layout 等 94 个
传递依赖），浏览器 global 名 `G6`（官方全局名），暴露 `G6.Graph`。
Wiki 知识图谱的第三套选型对照原型（`docs/prototypes/prototype-wiki-graph-g6.html`）用。

来源包：@antv/g6@5.1.1（MIT）+ AntV 传递依赖（均 MIT）。

复现命令：

```bash
npm install @antv/g6@5.1.1
cat > entry-g6.js <<'EOF'
import { Graph } from "@antv/g6";
export { Graph };
EOF
npx -y esbuild entry-g6.js --bundle --minify --format=iife \
  --global-name=G6 --legal-comments=inline --outfile=g6.min.js
```

使用注意（踩坑记录）：

- **G6 用图坐标当像素**：布局/位置 1 单位 = 1px，喂位置前把坐标归一化到目标画布尺度。
- **headless 截图**：virtual-time 下 rAF 挨饿，布局动画与元素入场 fade-in 会冻在半途
  （图半透明/布局停在散乱态）。页面加 `?noanim=1` 同时关根动画与布局动画——
  G6 布局 `animation: false` 时走 `execute+stop+tick(300)` 同步终态，截图确定。
- **@antv/layout force 的 nodeStrength 默认是正数 1000**（斥力），不是 d3 的负值约定。
- 聚类向心：`centripetalOptions.center` 支持**逐节点函数**返回 `{x,y,centerStrength}`，
  可作"锚回预计算位置"的钩子（本原型用法）；`clustering+nodeClusterBy` 的类目聚类
  实测对本数据（大量 deg 0 节点）不成簇。
- 力导拖拽回弹用 `drag-element-force` 行为（配合 force 布局）。

## sigma-graph.min.js

Sigma.js + graphology 的单文件 IIFE bundle，浏览器 global 名 `WikiGraphVendor`，
暴露 `{ Sigma, Graph, forceAtlas2, DiamondNodeProgram, DashedEdgeProgram }`。
供 Wiki 知识图谱（GUI 及原型）使用。

`DiamondNodeProgram`（菱形节点，带白色描边）与 `DashedEdgeProgram`（屏幕空间
虚线边）是项目自写 program，源码在构建目录 `entry.js` 中（随 bundle 打入）。

来源包（均 MIT License）：

| 包 | 版本 | 说明 |
|---|---|---|
| sigma | 3.0.3 | WebGL 图渲染 |
| graphology | 0.26.0 | 图数据结构 |
| graphology-layout-forceatlas2 | 0.10.1 | ForceAtlas2 布局 |

传递依赖（随 bundle 打入）：events、graphology-utils、graphology-types。

文件头部注释块保留了上述包的 MIT license 归属声明。

## 复现命令

```bash
mkdir vendor-build && cd vendor-build
npm init -y
npm install sigma@3.0.3 graphology@0.26.0 graphology-layout-forceatlas2@0.10.1

cat > entry.js <<'EOF'
import Sigma from "sigma";
import Graph from "graphology";
import forceAtlas2 from "graphology-layout-forceatlas2";
export { Sigma, Graph, forceAtlas2 };
EOF

npx -y esbuild entry.js --bundle --minify --format=iife \
  --global-name=WikiGraphVendor --legal-comments=inline \
  --outfile=../web/vendor/sigma-graph.min.js
# 注意 outfile 相对路径按实际目录调整；打完后在文件头补上 license 注释块
#（esbuild 的 legal-comments 只保留源码中已有的 /*! 注释，这三个包的 dist 没有，需手动补）
```

## 使用注意（踩坑记录）

- **sigma v3 相机作用于归一化坐标**：sigma 内部把图坐标按 bbox 归一化到
  framed 空间（bbox 中心 → `(0.5, 0.5)`，最长边 → 1）。要 fit 整图：
  `renderer.getCamera().setState({ x: 0.5, y: 0.5, ratio: 1.06 })`，
  不要拿原始图坐标去算 ratio。
- **拖拽节点**用 `renderer.viewportToGraph(e)`（返回原始图坐标），模板见
  `docs/prototypes/prototype-wiki-graph-sigma.html`。
- **headless 截图需要 WebGL**：Edge headless 不要加 `--disable-gpu`
  （必要时加 `--use-gl=angle`），否则黑图。
- **Node 里 smoke test**：bundle 在模块求值时引用 `WebGL2RenderingContext`，
  需 stub；浏览器无此问题。
