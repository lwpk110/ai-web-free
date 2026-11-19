# AMD Ryzen 7 7840HS + 780M 核显：ComfyUI 与 AI 短剧 / TTS 本地完整部署方案

> **设备基准**：AMD Ryzen 7 7840HS (8核16线程) | AMD Radeon 780M (RDNA3 核显) | 32GB 内存（建议/可升级至 64GB） | D 盘（剩余 ~550GB）

---

## 目录
1. [前期硬件与系统调优](#一-前期硬件与系统调优)
2. [环境准备与目录规划](#二-环境准备与目录规划)
3. [ComfyUI 内核与 DirectML 核显加速部署](#三-comfyui-内核与-directml-核显加速部署)
4. [核心扩展节点安装清单（短剧与视频）](#四-核心扩展节点安装清单短剧与视频)
5. [TTS 语音合成与声音克隆环境配置](#五-tts-语音合成与声音克隆环境配置)
6. [推荐轻量模型下载清单（防爆显存/高速度）](#六-推荐轻量模型下载清单防爆显存高速度)
7. [一键启动脚本与日常维护](#七-一键启动脚本与日常维护)

---

## 一、 前期硬件与系统调优

AMD APU（7840HS）的特点是**核显与 CPU 共享系统物理内存**。为了让 ComfyUI 和 DirectML 稳定跑大模型不崩溃，建议开机做一次 BIOS 显存预分配设置：

1. **BIOS 调整 UMA Frame Buffer Size（显存预设）**：
   * 开机按 `F2` 或 `Del` 键进入 BIOS；
   * 找到 `Advanced`（高级）-> `AMD CBS` -> `NBIO Common Options` -> `GFX Configuration`（不同主板位置略有差异）；
   * 将 `UMA Frame Buffer Size` 从默认的 `Auto` 或 `2G` 改为：
     * **当前 32G 内存**：推荐设为 **4GB 或 8GB**；
     * **未来升级 64G 内存**：推荐设为 **8GB 或 16GB**；
   * 保存并重启系统。

> [!TIP]
> 即使在 Windows 下显存会自动动态扩展，预先分配 4G~8G 能有效防止部分 PyTorch 算子因初始化显存不足而报错闪退。

---

## 二、 环境准备与目录规划

> [!CAUTION]
> 本机 **C 盘剩余不足 18GB**，切勿将 ComfyUI 或任何模型下载到 C 盘。所有文件统一存放于空间充裕的 **D 盘**。

### 1. 目录结构规范
在 D 盘创建以下工作区：
```text
D:\AI_Studio\
├── ComfyUI\              # ComfyUI 本体
├── Models_Cache\         # 公共模型缓存（便于备份与跨软件共享）
└── TTS_Audio\            # 配音与音效导出目录
```

### 2. 基础工具准备
本机已预装 `Git`，建议安装 **Miniconda**（轻量级 Python 环境管理器）：
1. 访问 [Miniconda 官网下载页面](https://docs.anaconda.com/miniconda/) 下载 Windows 安装包；
2. 安装时选择路径为 `D:\AI_Studio\Miniconda3`，并勾选 `Add to PATH`（或使用其专属控制台）。

---

## 三、 ComfyUI 内核与 DirectML 核显加速部署

推荐通过 **Conda 虚拟环境 + DirectML 版 PyTorch** 进行部署，这是目前 AMD 核显在 Windows 下性能和兼容性最好的方案。

### 步骤 1：创建并激活独立 Python 3.11 环境
打开终端（PowerShell 或 CMD），执行以下命令：
```powershell
# 创建名为 comfy 的独立环境（Python 3.11 兼容性最佳）
conda create -n comfy python=3.11 -y

# 激活环境
conda activate comfy
```

### 步骤 2：安装 DirectML 支持的 PyTorch
```powershell
# 安装 PyTorch CPU 基础版 + Torch-DirectML 驱动
pip install torch torchvision torchaudio --index-url https://download.pytorch.org/whl/cpu
pip install torch-directml
```

### 步骤 3：拉取 ComfyUI 源码并安装依赖
```powershell
# 进入 D 盘项目目录
cd D:\AI_Studio
git clone https://github.com/comfyanonymous/ComfyUI.git
cd ComfyUI

# 安装运行基础依赖
pip install -r requirements.txt
```

---

## 四、 核心扩展节点安装清单（短剧与视频）

通过 Git 插件直接将必备节点安装到 `D:\AI_Studio\ComfyUI\custom_nodes\` 目录下：

```powershell
cd D:\AI_Studio\ComfyUI\custom_nodes

# 1. 节点管理器（极其重要：可在 UI 界面点选安装其他所有插件和缺失节点）
git clone https://github.com/ltdrdata/ComfyUI-Manager.git

# 2. 角色一致性神器（短剧固定长相必须）
git clone https://github.com/cubiq/ComfyUI_IPAdapter_plus.git
git clone https://github.com/ZHO-ZHO-ZHO/ComfyUI-InstantID.git

# 3. 高级控制网络（控制肢体、构图与线稿）
git clone https://github.com/Fannovel16/comfyui_controlnet_aux.git

# 4. 数字人说话与口型对齐（输入音频即可让静态人像开口对口型）
git clone https://github.com/KwaiVGI/LivePortrait.git

# 5. 动画与镜头推拉（制作动态分镜）
git clone https://github.com/Kosinkadink/ComfyUI-AnimateDiff-Evolved.git
```

---

## 五、 TTS 语音合成与声音克隆环境配置

为短剧角色配音，推荐使用以下两个节点：

### 1. ComfyUI-Edge-TTS（零硬件开销，即插即用）
* **特点**：调用微软云端免密接口，不占本地 CPU/显存，生成毫秒级，音质自然。
```powershell
cd D:\AI_Studio\ComfyUI\custom_nodes
git clone https://github.com/AIGODLIKE/ComfyUI-Edge-TTS.git
```

### 2. ComfyUI-CosyVoice 或 F5-TTS（支持 3~5 秒真人声音克隆）
* **特点**：适合一人分饰多角，读取一段参考音频即可复刻声线。7840HS CPU 支持 AVX-512，本地推理速度接近实时。
```powershell
cd D:\AI_Studio\ComfyUI\custom_nodes
git clone https://github.com/warmshao/ComfyUI-CosyVoice.git
```

---

## 六、 推荐轻量模型下载清单（防爆显存/高速度）

所有模型文件均存放在 `D:\AI_Studio\ComfyUI\models\` 对应子目录下：

| 模型分类 | 推荐模型名 | 适用场景 | 存放路径 |
| :--- | :--- | :--- | :--- |
| **基础底模 (SD 1.5)** | `v1-5-pruned-emaonly.safetensors` 或 `Realistic Vision V6.0` | 极速出图（3~5秒/张），角色打样首选 | `models/checkpoints/` |
| **加速底模 (SDXL)** | `SDXL-Turbo` / `SDXL-Lightning (4-step)` | 高画质电影质感，只需 4~8 步（10~15秒/张） | `models/checkpoints/` |
| **超大模型 (64G内存)**| `FLUX.1-schnell-GGUF (Q4_K_M)` | 目前质感天花板，量化后大内存可无压力运行 | `models/unet/` |
| **角色锁定** | `ip-adapter-plus_sd15.safetensors` / `instantid` | 锁定主角五官特征与发型 | `models/ipadapter/` |
| **口型驱动** | `LivePortrait` 官方权重 | 驱动角色说话做微表情 | 运行后自动或手动放入 `models/liveportrait/` |

---

## 七、 一键启动脚本与日常维护

为了方便每次开机一键启动，在 `D:\AI_Studio\` 目录下新建一个名为 `run_comfyui.bat` 的批处理脚本：

### 启动脚本代码 (`D:\AI_Studio\run_comfyui.bat`)
```bat
@echo off
title ComfyUI - AMD 780M DirectML Acceleration
chcp 65001 >nul

echo ========================================================
echo 正在激活 Comfy 虚拟环境并启动 ComfyUI (DirectML 模式)...
echo ========================================================

cd /d D:\AI_Studio\ComfyUI
call conda activate comfy

:: --directml 调用 780M 核显算力
:: --fast 优化内存卸载速度
:: --listen 允许局域网或本地浏览器访问
python main.py --directml --fast --windows-standalone-build

pause
```

### 常见问题与运维 Tips
1. **浏览器访问**：启动成功后，打开浏览器访问 `http://127.0.0.1:8188`。
2. **遇错降级模式**：若某些冷门自定节点与 DirectML 算子冲突报错，可临时把启动脚本中的 `--directml` 改为 `--cpu`，依靠 7840HS 强劲的 CPU 算力保障任务完成。
3. **工作流分享**：遇到别人的优秀短剧/换脸工作流，只需将成品的 `.png` 样图直接拖入浏览器 ComfyUI 界面中，所有节点链路和参数就会自动还原。
