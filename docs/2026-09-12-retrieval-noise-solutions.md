# 短/口语查询关键词误命中：业界方案调研

日期：2026-09-12
调研范围：搜索引擎（Elasticsearch/OpenSearch/Lucene）、向量数据库（Weaviate/Qdrant/Vespa）、RAG 框架与论文（Self-RAG/Adaptive-RAG/CRAG/HyDE）、AI 记忆系统（mem0/Letta/Zep/OpenViking）、SQLite FTS5

> **背景与问题一句话复述**：Okryptos 的关键词通道把连续 CJK 切成 bigram、词元间 OR 匹配、BM25 归一化分 ≥ 0.5 即准入；短口语查询 "还是黑底的" 被切成 还是/是黑/黑底/底的 四个 bigram，虚词 "还是" 和跨词界假词 "底的"（稀有 → idf 高）命中无关 changelog 条目过了阈值，语义通道正确拒绝但按设计无法否决关键词准入，导致无关知识被注入。

---

## 1. CJK 分词与停用词

### 1.1 官方对 cjk analyzer（bigram 方案）的态度：定性劝退，建议换 ICU

- **Elasticsearch 官方在 language analyzer 文档中直接注明**："You may find that `icu_analyzer` in the ICU analysis plugin works better for CJK text than the `cjk` analyzer. Experiment with your text and queries."
  来源：https://www.elastic.co/docs/reference/text-analysis/analysis-lang-analyzer
- **OpenSearch ICU 文档更明确**："`icu_analyzer` provides superior word boundary detection for CJK text compared to the `cjk` analyzer's bigram tokenization method." 并在对比表中把 cjk 标注为 "Bigram tokenization (overlapping 2-character sequences)"，icu 标注为 "language-aware" 分词。
  来源：https://docs.opensearch.org/latest/analyzers/language-analyzers/icu/
- ES 内置 `cjk` analyzer 的等效配置是 `standard tokenizer + cjk_width + lowercase + cjk_bigram + stop filter`，但 **stop filter 默认停用词表是英文词**（a/and/the/...），没有任何中文虚词 bigram 处理。
  来源：https://www.elastic.co/docs/reference/text-analysis/analysis-lang-analyzer

**对本事故的直接含义**：业界对"bigram 切分产生跨词界假词、虚词误命中"的态度不是给 bigram 方案打补丁，而是换成分词质量更高的 tokenizer。

### 1.2 词典/概率分词的现成方案

- **smartcn**（Lucene Smart Chinese module，ES/OpenSearch 官方插件）：先断句再用概率模型求最优词序列，官方描述 "uses probabilistic knowledge to find the optimal word segmentation"。已知局限：对未登录词（新词、Emoji）有问题（elastic/elasticsearch#55913）。
  来源：https://www.elastic.co/guide/en/elasticsearch/plugins/current/analysis-smartcn.html
  来源：https://github.com/elastic/elasticsearch/issues/55913
- **IK analyzer**（社区事实标准，infinilabs 维护）：`ik_smart`（粗粒度）/ `ik_max_word`（细粒度）两种模式，支持自定义词典与热更新——这正好可以解 "底的" 这类问题：词典里没有 "底的" 这个词，就不会产出这个 token。
  来源：https://github.com/infinilabs/analysis-ik
- **Go 侧等价物**：Go 生态的词典分词（如 gojieba 等）属于二手知识，本调研未对具体 Go 库做一手验证；方向上"bigram → 词典分词 + 未登录词回退"在 ES/IK 侧是成熟做法。

### 1.3 停用词在 BM25 时代的官方立场演变

Elasticsearch: The Definitive Guide 的 "Stopwords: Performance Versus Precision" / "Pros and Cons of Stopwords" 章节完整记录了这个演变：

- 早期检索时代停用词的目的是**缩小索引、节省磁盘**（"It was essential to make your index as small as possible"）。
- 现在 "Using stopwords for the sake of reducing index size is **no longer a valid reason**"——每百万文档只省约 4MB。
- 移除停用词会损害精度（无法区分 "happy" 与 "not happy"、搜不到 "To be, or not to be"）；现代做法是**保留常用词可搜、靠 scoring 处理权重**——BM25 的 idf 天然给高频词低权重，无需删除。

来源：https://www.elastic.co/guide/en/elasticsearch/guide/current/stopwords.html
来源：https://www.elastic.co/guide/en/elasticsearch/guide/current/pros-cons-stopwords.html

**对本事故的反向含义**：通用停用词对 BM25 已非必需（idf 自动降权高频词），但本事故的肇事者恰恰相反——"底的" 是**稀有词，idf 高**，停用词逻辑（针对高频词）对它无效；"还是" 是高频虚词，idf 低，但仍贡献了准入所需的词元数。也就是说：**问题不只在分数，还在"命中了就算数"的 OR 准入结构**（见第 2 节）。

### 1.4 bigram vs 词典分词的中文检索精度定量对比

**未找到一手来源的定量对比**（官方只有 1.1 的定性劝退）。社区普遍经验是词典分词 precision 更高、bigram recall 更高，但无权威 benchmark 可引。

---

## 2. BM25 误命中抑制

### 2.1 minimum_should_match：业界对付"OR 匹配中部分词元命中"的标准机制

- ES/OpenSearch 官方：`minimum_should_match` 支持整数、百分比（`75%`，向下取整）、负百分比、条件组合（`3<90%`、`2<-25% 9<-3`）。
  来源：https://www.elastic.co/docs/reference/query-languages/query-dsl/query-dsl-minimum-should-match
  来源：https://docs.opensearch.org/latest/query-dsl/minimum-should-match/

**对 "还是黑底的" 的反演**：4 个 bigram 的查询，若要求 `2<-25%`（≤2 词全要、>2 词允许缺 25%），需要命中 ≥3 个 bigram——本事故只命中 2 个（还是、底的），**会被直接挡住**。这正是业界对此类问题的第一道防线：不看绝对分数，看"命中词元占比"。

### 2.2 BM25 分数阈值：官方明确定性为不可移植

- **Weaviate 官方 hybrid 文档**：hybrid 搜索提供 `max vector distance` 阈值且**只作用于向量组件**；对 BM25 组件和融合后分数**刻意不提供阈值参数**，原话："This is because BM25 scores are not normalized or bounded like vector distances, making a universal threshold less meaningful."
  来源：https://docs.weaviate.io/weaviate/concepts/search/hybrid-search
- Elasticsearch 的 ranking 文档把 BM25 定位为**第一阶段排序信号**，分数的用途是排序与产出候选集，绝对分值交由后段 re-ranking 处理。
  来源：https://www.elastic.co/docs/solutions/search/ranking

**对本系统的含义**：当前的 `min_score(0.5)` 归一化绝对阈值在业界没有背书；可移植的做法是**相对信号**（命中词元数/占比、与查询内最高分的相对差距、rank 位置）而非绝对分数。

### 2.3 idf 异常（稀有假词高分）

- BM25 的 idf 奖励稀有词是算法设计本身（Robertson 概率框架）；ES 侧唯一的官方调参面是 similarity 模块（k1/b 调整、或 scripted similarity 自定义打分），**没有"稀有词降权"开关**。
  来源：https://www.elastic.co/docs/reference/elasticsearch/mapping-reference/similarity
- 业界对"稀有但无意义词元"（如跨词界 bigram）的实际解法在**分词层**（让假词根本不被产出，见 1.2），而非打分层。打分层能做的是 2.1 的命中数门槛——单个稀有假词的高 idf 无法独自凑够命中数。

---

## 3. 混合检索准入与融合

### 3.1 RRF 原论文的立场：纯排序聚合，不含任何准入/否决语义

Cormack, Clarke, Büttcher, SIGIR 2009（已读全文）：

- RRF 的目标是"combine the document rankings from multiple IR systems, consistently yields better results than any individual system"，平均比最好单系统好 4-5%。
- 常数 `k=60` 的作用是 "**mitigates the impact of high rankings by outliers**"——缓解离群高排名文档的影响，而不是过滤它们。
- 论文明确 RRF 相对 Condorcet 的优势在于 "One or two systems that rank a document highly can substantially improve its rank"——**RRF 的设计哲学就是信任每个通道的输出列表**，单通道排第一的文档合法地获得 1/(60+1) 的贡献。论文中没有任何"某通道可以否决另一通道"的概念。

来源：https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf

**对本系统的含义**：用 RRF 只排序是符合原论文精神的；但"准入"（决定注入与否）本来就不是 RRF 的职责域，需要融合之外的机制。本系统的"通道独立准入"设计在 RRF 文献中无先例可循，属于自行扩展。

### 3.2 向量数据库官方 hybrid 建议

- **Weaviate**：`alpha` 参数控制向量/关键词权重（0=纯关键词，1=纯向量，**服务端默认 0.75 偏向量**）；`max vector distance` 阈值只作用于向量组件，效果是"**向量距离过大的对象即使关键词分高也会被排除**"——官方文档原话 "objects with a vector distance higher than 0.3 will be excluded from the hybrid search results, even if they have high keyword search scores"。
  来源：https://docs.weaviate.io/weaviate/concepts/search/hybrid-search
  **这是业界现成的"语义通道可否决关键词命中"模式**：以语义质量作为整个 hybrid 结果集的准入下限。注意方向与 Okryptos 需求一致（语义做否决方），只是实现为距离阈值而非通道协商。
- **Qdrant**：`prefetch` 分别查 dense/sparse，再用 `rrf`（支持 per-ranking 权重 `w_r`，可做按查询动态加权）或 `dbsf`（Distribution-Based Score Fusion，按各通道分数分布归一化后融合）。
  来源：https://qdrant.tech/documentation/concepts/hybrid-queries/
- **Vespa**：官方 hybrid 教程用 cross-hits feature normalization + RRF；phased ranking 支持 `rank_score_drop_limit`（相对第一名分数丢弃过低者）——又一个"相对而非绝对"的门槛实例。
  来源：https://vespa-engine.github.io/pyvespa/getting-started-pyvespa.html
  来源：https://docs.vespa.ai/en/ranking/phased-ranking.html

### 3.3 反向模式（关键词否决语义）与双向要求

未找到主流系统提供"关键词通道否决语义命中"的一手实现。主流做法是**在融合后加 reranker 作为最终否决**（Elastic semantic re-ranking、Zep/mem0 的 rerank，见第 5 节）——即否决权交给一个更强的评判模型，而不是交给另一个单通道。

来源：https://www.elastic.co/docs/solutions/search/ranking

---

## 4. 查询理解前置（先判断值不值得检索 / 先改写查询）

### 4.1 Retrieval gating / Adaptive retrieval（与本事故最对症的研究线）

- **Self-RAG**（Asai et al., ICLR 2024）：训练模型生成 reflection token，**按需决定是否检索**（"adaptively retrieves passages on-demand"），避免对所有输入无差别检索注入。
  来源：https://arxiv.org/abs/2310.11511
- **Adaptive-RAG**（Jeong et al., NAACL 2024）：训练分类器按查询复杂度在三档策略间路由——**不检索 / 单步检索 / 多步检索**；简单查询（"还是黑底的" 这类）直接不检索。
  来源：https://arxiv.org/abs/2403.14403
- **CRAG**（Yan et al., 2024）：引入 retrieval evaluator 给检索结果打 correct/ambiguous/incorrect，判为 incorrect 则**丢弃检索结果**并触发纠正动作——"检索后发现是垃圾就扔掉"的成熟模式。
  来源：https://arxiv.org/abs/2401.15884

这三个方案的共同思想：**检索准入决策本身是一个需要显式建模的判断**，而不是检索器的副产品。其中 Adaptive-RAG 的轻量版（不用训练分类器，用查询特征规则）对本系统无 LLM 的 hook 路径也可落地。

### 4.2 短查询改写 / 扩展

- **Rewrite-Retrieve-Read**（Ma et al., 2023）：先让 LLM 把短/模糊查询改写成更完整的检索查询再检索。
  来源：https://arxiv.org/abs/2305.14283
- **HyDE**（Gao et al., ACL 2023）：LLM 生成假想答案文档，嵌入**假想答案**而非原始短查询去检索，解决"短问题与长文档在嵌入空间距离远"的分布不匹配。
  来源：https://arxiv.org/abs/2212.10496
  Elastic 官方实验博客报道 HyDE 在 "short, casual queries" 上对语义搜索精度有显著提升（声称 ~50%，为官方博客数字而非论文数字）。
  来源：https://www.elastic.co/search-labs/blog/hyde-semantic-search-elasticsearch
- **框架层**：LlamaIndex 官方提供 Query Transformations 模块（rewrite/HyDE/step-back 等）；LangChain 有 MultiQueryRetriever（LLM 生成多个查询变体、分别检索后合并）。
  来源：https://developers.llamaindex.ai/python/framework/optimizing/advanced_retrieval/query_transformations/
  来源：https://api.python.langchain.com/en/latest/retrievers/langchain_community.retrievers.weaviate_hybrid_search.WeaviateHybridSearchRetriever.html

**对本系统的约束提醒**：仓库既定方针是 hook 路径不加 LLM（延迟敏感，见 docs/superpowers/specs/2026-08-16-retrieval-evolution.md），因此 4.2 的 LLM 方案只适用于 `ok search` 交互路径；4.1 的"值不值得检索"判断可以用**无 LLM 的规则版**落地。

---

## 5. AI 记忆/知识注入系统（与本系统最同类）

### 5.1 mem0：threshold + rerank 双保险，分数细节可观测

官方 Search 文档：检索管线为 query processing → vector search → filtering & reranking；`threshold` 控制最小相似度；rerank 是显式的"二次打分提升精度"步骤；`explain=True` 返回 `score_details`（semantic score、normalized BM25、entity boost、final score、**threshold used for filtering**）——即 mem0 官方就提供了"阈值准入 + rerank 精排 + 分数可解释"三件套。
来源：https://docs.mem0.ai/core-concepts/memory-operations/search
来源：https://docs.mem0.ai/open-source/features/reranker-search

### 5.2 Zep / Graphiti：高召回搜索 + reranker 精排 + 质量阈值过滤

- Zep 论文（arXiv:2501.13956）：检索 = search → rerank → construct 三阶段；search 用 cosine + BM25 + graph BFS 三路 RRF 融合**求高召回**，"rerankers serve to increase precision"。
  来源：https://arxiv.org/html/2501.13956v1
- 官方文档：reranker score 可用于**手动设阈值过滤结果**（"filter results to only include those above a certain relevance threshold"）；另有 fact rating 机制，注入记忆时可设最低 fact rating，低质量事实直接不进上下文。
  来源：https://help.getzep.com/searching-the-graph
  来源：https://help.getzep.com/v2/concepts

### 5.3 Letta / MemGPT：不每轮注入，由 agent 按需检索

MemGPT 架构（arXiv:2310.08560）的核心决策是把检索**从"每轮强制注入"改为"agent 通过 function call 自主决定何时查 archival memory"**——需求驱动（pull）而非推送驱动（push）。这是与本系统 hook 每轮强制检索注入**根本不同**的范式：噪声问题的第一性解法是"不相关时不检索"，MemGPT 把这个判断交给 LLM 本身。
来源：https://arxiv.org/pdf/2310.08560 （MemGPT 论文；agentic memory management via function calls 的架构亦有大量官方文档佐证，如 https://docs.letta.com）

### 5.4 OpenViking（已有本仓库调研，此处仅引结论）

本仓库 `docs/2026-08-19-openviking-retrieval-review.md` 已有一手源码级调研，与本事故相关的结论：
- rerank 阈值 0.1 准入 + 按排序花预算，**不设绝对加深阈值**（实测分数带太窄，0.38-0.50）——与 OK 动态 SemanticFloor 互相印证"绝对分数阈值不可靠"。
- 模型报 NO_RELEVANT_MEMORY 时整块置空——"宁缺毋滥"。
- 跨轮冷却台账 RecallLedger 去重注入。
- IntentAnalyzer（LLM）做多 TypedQuery 意图分析，但扩展上限 3 条、5s 超时、失败闭环回原 query。

来源：docs/2026-08-19-openviking-retrieval-review.md（本仓库一手调研）；OpenViking 源码 https://github.com/volcengine/OpenViking

### 5.5 小结

四个同类系统里，**没有一个用 BM25 绝对分数阈值做准入**；共同配方是「相对信号准入 + reranker/LLM 精排否决 + 宁缺毋滥的兜底（整块置空）」。

---

## 6. SQLite FTS5 专项

### 6.1 trigram tokenizer（3.34+）对 CJK

官方文档：

- trigram tokenizer "treats each contiguous sequence of three characters as a token, allowing FTS5 to support more general **substring matching**"——查询词元可匹配行内任意字符序列，不限于完整 token。
- 支持 `case_sensitive`、`remove_diacritics` 选项；未设 remove_diacritics 时 trigram 表还支持**索引加速的 GLOB/LIKE 匹配**（`WHERE a LIKE '%cdefg%'`）。
- 文档未对 CJK 专门说明；trigram 把 CJK 当普通字符流切三元组，天然支持 CJK 子串匹配。

来源：https://www.sqlite.org/fts5.html （§4.3.4 The Trigram Tokenizer）

**对 Okryptos 的评估**（结合自切 bigram 现状）：
- trigram 同样是 n-gram，**同样无词界概念**——"还是黑底的" 会切成 还是黑/是黑底/黑底的，"底的" 这类假词问题并未消失，只是粒度从 2 变 3（假词概率降低但不消除）；且 trigram 词元数更多、idf 分布不同，BM25 分数特性会变，现有阈值校准作废。
- 第三方（非一手）有报告称 trigram + bm25() 对 CJK 查询出现过 textScore=0 的实现 bug（openclaw/openclaw#92061），未独立验证，仅提示迁移需回归测试。
  来源：https://github.com/openclaw/openclaw/issues/92061
- trigram 的真正增量价值是 **LIKE/GLOB 子串匹配索引化**（做精确片段定位），不是相关性排序。

### 6.2 bm25() 调参与 rank

官方文档（§5.1.1）：

- `bm25()` 返回值**越小越好**（实现乘了 -1 使 `ORDER BY bm25(t)` 升序即最佳在前）；`ORDER BY rank` 与 `ORDER BY bm25(t)` 等价且更快。
- **列权重**：`bm25(email, 10.0, 5.0)` 给各列分配权重（示例：sender 命中顶 10 次 body 命中）——Okryptos 若有 title/tags 与 body 分列，这是官方支持的加权手段。
- rank 列可用 `INSERT INTO ft(ft, rank) VALUES('rank', 'bm25(10.0, 5.0)')` 改持久默认。
- 官方对 k1/b **没有暴露调参口**（FTS5 的 BM25 参数不可调），对阈值无任何建议——再次印证"BM25 绝对阈值无官方背书"。

来源：https://www.sqlite.org/fts5.html （§5.1.1 The bm25() function）

### 6.3 用查询语法实现 minimum_should_match 语义

FTS5 查询语法支持 AND / OR / NOT / 短语 / NEAR（官方文档 §3 Full-text Query Syntax）。因此可以在**查询构造层**实现 2.1 的机制：把 "t1 OR t2 OR t3 OR t4" 改为要求至少 N 个词元命中的组合查询，或在应用层对每个候选回查命中词元数。这不需要归一化 min_score 绝对阈值。

来源：https://www.sqlite.org/fts5.html

---

## 7. 横向对比表

| 方案 | 一手来源 | 对 Okryptos（Go+FTS5）适用性 | 改动量 |
|---|---|---|---|
| 换词典分词（IK/smartcn 思路 → Go 词典分词） | ES/OpenSearch 官方对 bigram 的定性劝退 | 治本（"底的" 不再产生），但引入词典依赖与未登录词问题；CJK+拉丁混合需回归 | 大 |
| minimum_should_match 式命中数门槛 | ES/OpenSearch 官方文档 | 直接对症：单个稀有假词无法凑够命中数；FTS5 查询语法或应用层计数即可实现 | 小 |
| 移除/弱化 BM25 绝对 min_score，改相对信号 | Weaviate 官方（BM25 无界、阈值无意义）；Vespa rank_score_drop_limit | 与命中数门槛配套：准入看命中占比，不看绝对分 | 小 |
| 中文 bigram 虚词停用表 | ES 停用词演变文档（机制），中文 bigram 停用表无现成一手来源 | 需自建小词表（还是/就是/底的类虚词 bigram）；只挡高频虚词，挡不住稀有假词 | 小-中 |
| 语义否决关键词（Weaviate max vector distance 模式） | Weaviate 官方文档 | 模式现成，但与"通道独立准入"的故意设计冲突，需产品决策；可弱化为"语义低分降级注入" | 中 |
| RRF 加权（Qdrant w_r / Weaviate alpha 动态化） | Qdrant/Weaviate 官方文档 | 只影响排序不影响准入，对本事故无效；可作为后续排序优化 | 中 |
| Retrieval gating 规则版（纯虚词/过短查询不检索） | Self-RAG/Adaptive-RAG 论文（思想），规则版为本调研推导 | 无 LLM 即可实现：查询去停用后无有效内容词 → 跳过注入 | 小 |
| LLM 查询改写/HyDE | HyDE/Rewrite-Retrieve-Read 论文；LlamaIndex 文档 | 仅 `ok search` 路径可用（hook 路径禁用 LLM 是既定方针） | 中（限 CLI） |
| reranker 二次打分否决 | Zep/mem0/Elastic 官方 | 本系统万条级本地库，cross-encoder 成本可接受但引入模型依赖；属于精排增强而非准入门 | 大 |
| FTS5 trigram 迁移 | SQLite 官方文档 | 无词界问题依旧，粒度变化使阈值全部重校准，收益主要是 LIKE/GLOB；不推荐为降噪手段 | 中-大 |
| bm25() 列权重（title 加权） | SQLite 官方文档 | 若 title/tags 与 body 分列，标题命中加权可挤压正文假词命中的排名，间接降噪 | 小 |
| 不相关整块置空（宁缺毋滥兜底） | OpenViking（本仓库既有调研）；Self-RAG | 与现有"语义无显著头部则整体拒绝"同思路，可推广到关键词通道：命中全部为停用词元时不注入 | 小 |

---

## 8. 针对本系统的推荐组合方案

### 方案 A：最小改动、纯 Go+SQL、不解构现有设计（首选）

三步都只做在关键词通道准入内部，不动"通道独立准入"架构：

1. **命中数门槛替代/补充 min_score**：bigram 查询要求命中 ≥ max(2, ⌈词元数 × 50%⌉) 个不同词元（minimum_should_match 语义）。对 "还是黑底的"（4 词元需命中 2 个，且……见第 2 步）——注意需配合第 2 步，否则 还是+底的 恰好凑够 2。
2. **有效词元过滤（虚词停用表，仅作用于准入计数）**：自建中文虚词 bigram 小表（还是、就是、底的、了的、我们 等），命中停用词元不计入第 1 步的命中数。则 "还是黑底的" 的有效词元 = 是黑/黑底（2 个），候选条目需同时命中这 2 个——本事故的无关 changelog 命中数降为 0-1，**无法准入**。依据：ES 停用词机制 + 业界对"命中质量"而非"命中分数"的重视（Weaviate 对 BM25 阈值的定性）。
3. **空查询 gate**：停用过滤后有效词元为 0 的查询（纯口语虚词）直接跳过关键词注入（Self-RAG/Adaptive-RAG 的规则降级版）。

预期效果：本事故被第 1+2 步直接消除；对正常含实词的查询召回影响小（实词仍需命中，虚词不再"助攻"）。

### 方案 B：A + 语义降级（需推翻一条设计约束）

在 A 之上加 Weaviate `max vector distance` 同款模式：**关键词通道准入的条目，若其语义余弦低于该查询的动态基线（现有 SemanticFloor 已具备此计算），则注入时降级为"弱相关提示"或不注入**。

- 依据：Weaviate 官方文档明确支持"语义不达标者即使关键词分高也排除"；Zep/mem0/Elastic 的 reranker 否决是同思想的模型化版本。
- 成本：与"关键词准入不可被语义否决（故意设计）"直接冲突。本调研的立场：RRF 原论文不提供任何"通道独立准入"的依据，该设计无文献背书；业界主流是把否决权交给更强的信号。建议至少允许"语义确认缺失 → 注入降级标注"，而不是完全二元否决。

### 方案 C：结构性方案（长期）

- **分词层**：引入 Go 词典分词（IK 思路），bigram 降级为未登录词回退通道。依据：ES/OpenSearch 官方对 bigram 的定性劝退。改动最大，需全量重建索引与回归测试。
- **观察层**：接入检索健康统计（zero-result 率、关键词准入后被模型忽略率），用数据驱动停用表与门槛调参。依据：OpenViking retrieval_stats、mem0 explain=True 的做法。

**不建议**：迁移 FTS5 trigram 作为降噪手段（无词界问题依旧、阈值全部重校准）；引入完整 cross-encoder reranker 进 hook 路径（成本与收益不匹配，万条级本地库用方案 A 已可覆盖主要噪声）。

---

## 附：未找到一手来源的声明

- 中文检索 bigram vs 词典分词的**定量**精度对比：未找到官方/论文一手 benchmark。
- 现成的中文 bigram 虚词停用词表：未找到一手来源（哈工大停用词表等为单字/词级且为二手转引）。
- "关键词通道否决语义通道"的主流系统实现：未找到一手来源。
- FTS5 trigram 在 CJK BM25 下的官方性能/正确性对比：官方文档无 CJK 专项论述；第三方 bug 报告（openclaw#92061）未独立验证。
