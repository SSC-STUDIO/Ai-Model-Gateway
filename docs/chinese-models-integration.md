# 国产大模型 API 接入指南

AI Model Gateway 支持主流国产大模型的快速接入，所有支持的厂商均采用 OpenAI 兼容协议。

## 支持的国产大模型

| 厂商 | 名称 | 接入方式 | 文档 |
|------|------|----------|------|
| 百度 | 文心一言 (ERNIE) | OpenAI兼容 | [千帆平台](https://qianfan.baidu.com/) |
| 字节跳动 | 豆包 (Doubao) | OpenAI兼容 | [火山方舟](https://console.volcengine.com/ark/) |
| 智谱AI | ChatGLM | OpenAI兼容 | [Open Platform](https://open.bigmodel.cn/) |
| 讯飞 | 星火 (Spark) | OpenAI兼容 | [讯飞开放平台](https://xinghuo.xfyun.cn/) |
| MiniMax | abab系列 | OpenAI兼容 | [MiniMax](https://platform.minimaxi.com/) |
| 月之暗面 | Kimi | OpenAI兼容 | [Moonshot](https://platform.moonshot.cn/) |
| 深度求索 | DeepSeek | OpenAI兼容 | [DeepSeek](https://platform.deepseek.com/) |
| 阶跃星辰 | StepFun | OpenAI兼容 | [StepFun](https://platform.stepfun.com/) |
| 商汤 | 日日新 (SenseChat) | OpenAI兼容 | [SenseNova](https://platform.sensenova.cn/) |

## 接入方式

### 方式一：通过管理界面编辑 provider

1. 打开管理界面 `http://127.0.0.1:18080/admin`
2. 登录后切到配置相关 tab
3. 在 provider 配置中手动补充对应厂商的 `base_url`、`api_key`、`models`
4. 保存配置并重新验证模型列表

### 方式二：配置文件手动添加

复制 `configs/chinese-models.yaml` 中的配置模板到 `config.v2.yaml` 的 `providers` 部分，填入你的 API Key。

## 各厂商接入详情

### 百度文心一言 (ERNIE)

```yaml
- name: baidu-ernie
  base_url: https://qianfan.baidubce.com/v2
  api_key: your-baidu-api-key
  models:
    - ERNIE-4.0-8K
    - ERNIE-3.5-8K
    - ERNIE-Speed-8K
```

**获取API Key**: [百度智能云千帆平台](https://qianfan.baidu.com/)

**模型列表**:
- `ERNIE-4.0-8K` - 百度旗舰模型
- `ERNIE-3.5-8K` - 性价比之选
- `ERNIE-Speed-8K` - 高速推理
- `ERNIE-Lite-8K` - 轻量级

### 字节跳动豆包 (Doubao)

```yaml
- name: doubao
  base_url: https://ark.cn-beijing.volces.com/api/v3
  api_key: your-doubao-api-key
  models:
    - ep-xxxxxxxxx  # 推理接入点ID
```

**获取API Key**: [火山方舟](https://console.volcengine.com/ark/)

**注意**: 豆包需要在控制台创建"推理接入点"，模型名称格式为 `ep-xxxxxxxxxx`。

### 智谱AI (ChatGLM)

```yaml
- name: zhipu-glm
  base_url: https://open.bigmodel.cn/api/paas/v4
  api_key: your-zhipu-api-key
  models:
    - glm-4
    - glm-4-plus
    - glm-3-turbo
```

**获取API Key**: [智谱开放平台](https://open.bigmodel.cn/)

### 讯飞星火 (Spark)

```yaml
- name: iflytek-spark
  base_url: https://spark-api-open.xf-yun.com/v1
  api_key: your-spark-api-key
  models:
    - spark-pro
    - spark-max
    - spark-lite
```

**获取API Key**: [讯飞开放平台](https://xinghuo.xfyun.cn/)

### MiniMax

```yaml
- name: minimax
  base_url: https://api.minimaxi.chat/v1
  api_key: your-minimax-api-key
  models:
    - abab6.5-chat
    - abab6.5s-chat
```

**获取API Key**: [MiniMax](https://platform.minimaxi.com/)

### 月之暗面 (Kimi)

```yaml
- name: kimi-official
  base_url: https://api.moonshot.cn/v1
  api_key: your-kimi-api-key
  models:
    - moonshot-v1-8k
    - moonshot-v1-32k
    - moonshot-v1-128k
```

**获取API Key**: [Moonshot](https://platform.moonshot.cn/)

### 深度求索 (DeepSeek)

```yaml
- name: deepseek-official
  base_url: https://api.deepseek.com/v1
  api_key: your-deepseek-api-key
  models:
    - deepseek-chat
    - deepseek-reasoner
```

**获取API Key**: [DeepSeek](https://platform.deepseek.com/)

## 桥接配置建议

建议配置桥接规则，将标准模型名映射到国产大模型：

```yaml
compat:
  bridge:
    enabled: true
    rules:
      # GPT-5 桥接到文心一言
      - from: gpt-5.4
        to: ERNIE-4.0-8K
      # Claude 桥接到 GLM
      - from: claude-opus-4-6
        to: glm-4
      # 其他桥接规则...
```

## 故障排查

### 401 Unauthorized
- 检查API Key是否正确
- 确认API Key有调用对应模型的权限

### 404 Not Found
- 检查模型名称是否正确
- 豆包需要确认推理接入点已创建且ID正确

### 连接超时
- 检查网络连接
- 确认base_url填写正确（部分厂商有多个地域入口）

## 参考

- [国产大模型配置模板](../../configs/chinese-models.yaml)
