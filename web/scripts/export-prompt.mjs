import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createLibrary, defineComponent } from "@openuidev/react-lang";
import {
  openuiChatComponentGroups,
  openuiChatLibrary,
  openuiChatPromptOptions,
} from "@openuidev/react-ui";
import { z } from "zod/v4";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const PoolOverviewCard = defineComponent({
  name: "PoolOverviewCard",
  description:
    "Displays real-time AI-Web-Free account pool statistics and health metrics for a provider (e.g. chatgpt) with a live probe action button.",
  props: z.object({
    provider: z.string(),
    title: z.string().optional(),
  }),
  component: () => null,
});

const AccountDiagnosticTable = defineComponent({
  name: "AccountDiagnosticTable",
  description:
    "Renders an interactive diagnostic table of accounts in the pool with live Probe, Refresh, and Enable/Disable actions.",
  props: z.object({
    provider: z.string(),
    statusFilter: z.string().optional(),
  }),
  component: () => null,
});

const ImageStudioCard = defineComponent({
  name: "ImageStudioCard",
  description:
    "Interactive GPT-Image-2.5 generation studio card. Can generate images directly inside the conversation using gpt-image-2.5-sunburst or gpt-image-2.5-flare.",
  props: z.object({
    prompt: z.string(),
    model: z.string().optional(),
    size: z.string().optional(),
    quality: z.string().optional(),
  }),
  component: () => null,
});

const baseCard = openuiChatLibrary.components.Card;
const origChildrenOptions =
  baseCard.props.shape.children._zod.def.element._zod.def.options;

const ExtendedCard = defineComponent({
  name: "Card",
  description: baseCard.description,
  props: z.object({
    children: z.array(
      z.union([
        ...origChildrenOptions,
        PoolOverviewCard.ref,
        AccountDiagnosticTable.ref,
        ImageStudioCard.ref,
      ])
    ),
    sources: baseCard.props.shape.sources,
  }),
  component: baseCard.component,
});

const aiWebFreeLibrary = createLibrary({
  root: "Card",
  components: [
    ...Object.values(openuiChatLibrary.components).filter(
      (c) => c.name !== "Card"
    ),
    ExtendedCard,
    PoolOverviewCard,
    AccountDiagnosticTable,
    ImageStudioCard,
  ],
  componentGroups: [
    ...openuiChatComponentGroups,
    {
      name: "AI-Web-Free Pool & Studio Widgets",
      components: [
        "PoolOverviewCard",
        "AccountDiagnosticTable",
        "ImageStudioCard",
      ],
    },
  ],
});

const prompt = aiWebFreeLibrary.prompt({
  ...openuiChatPromptOptions,
  preamble:
    "You are the AI-Web-Free Generative UI Assistant powered by Thesys OpenUI Lang. Always respond with valid OpenUI Lang starting with `root = Card([...])`. Combine built-in OpenUI components (CardHeader, TextContent, BarChart, LineChart, PieChart, Table, Tabs, Form, Callout, CodeBlock) and custom AI-Web-Free components (PoolOverviewCard, AccountDiagnosticTable, ImageStudioCard) to build rich, interactive interfaces.",
  additionalRules: [
    ...(openuiChatPromptOptions.additionalRules || []),
    "When the user asks about account pool status, health metrics, or provider overview, include PoolOverviewCard(\"chatgpt\", \"ChatGPT 免费号池实时健康大盘\") and/or AccountDiagnosticTable(\"chatgpt\") inside Card.",
    "When the user wants to generate or preview an image, include ImageStudioCard(prompt, \"gpt-image-2.5-sunburst\", \"1024x1024\", \"hd\") inside Card.",
  ],
});

const outPath = path.resolve(__dirname, "../system-prompt.txt");
fs.writeFileSync(outPath, prompt, "utf-8");
console.log(
  `Generated OpenUI Lang system prompt (${prompt.length} chars) -> ${outPath}`
);
