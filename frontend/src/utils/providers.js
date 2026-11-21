export const TRADITIONAL_PROVIDERS = [
  { id: "bing", label: "Bing" },
  { id: "google", label: "Google" },
  { id: "microsoft", label: "Microsoft" },
  { id: "yandex", label: "Yandex" },
  { id: "modernmt", label: "ModernMT" },
  { id: "alibaba", label: "Alibaba" },
  { id: "youdao", label: "Youdao" },
  { id: "papago", label: "Papago" },
  { id: "qqtransmart", label: "qqTranSmart" },
  { id: "qqimt", label: "qqimt" },
  { id: "translatecom", label: "Translate.com" },
  { id: "volcengine", label: "Volcengine" },
  { id: "lingocloud", label: "LingoCloud" },
];

export const ALL_PROVIDERS = [
  ...TRADITIONAL_PROVIDERS,
  { id: "offline_llm", label: "Local Offline LLM (llama.cpp)" },
  { id: "llm", label: "OpenAI-Compatible LLM" },
];
