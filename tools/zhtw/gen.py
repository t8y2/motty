#!/usr/bin/env python3
"""Generate internal/i18n/traditional.go from the Simplified Chinese catalog.

The Simplified catalog stays the source of truth, so a string added upstream
reaches zh-TW by running this again. OpenCC does the character and common-phrase
conversion; TERMS below then moves it to the words Taiwanese software uses. Fix
a wrong word by adding a term here: traditional.go is overwritten on every run.

    pip install opencc-python-reimplemented
    python tools/zhtw/gen.py && gofmt -w internal/i18n/traditional.go
"""
import pathlib
import re

import opencc

ROOT = pathlib.Path(__file__).resolve().parents[2]
I18N = ROOT / "internal" / "i18n"
OUTPUT = I18N / "traditional.go"
# Files that hold a `var chineseX = ...` block beside their message struct.
SIDE_CATALOGS = [
    "action_feedback.go",
    "code_editor.go",
    "dashboard.go",
    "local_proxy.go",
    "monitor.go",
    "session_logs.go",
    "ssh_sync.go",
]

# Applied in order after OpenCC, so a longer phrase must come before its parts.
TERMS = [
    ("賬號", "帳號"),
    ("賬戶", "帳戶"),
    ("用戶名", "使用者名稱"),
    ("用戶", "使用者"),
    ("文件夾", "資料夾"),
    ("標籤頁", "分頁"),
    ("分屏", "分割畫面"),
    ("全屏", "全螢幕"),
    ("回滾", "回捲"),
    ("滾動條", "捲軸"),
    ("滾動", "捲動"),
    ("私鑰", "私密金鑰"),
    ("公鑰", "公開金鑰"),
    ("密鑰", "金鑰"),
    ("證書", "憑證"),
    ("隧道", "通道"),
    ("高亮", "醒目提示"),
    ("日誌", "記錄"),
    ("鏈接", "連結"),
    ("數據", "資料"),
    ("激活", "啟用"),
    ("禁用", "停用"),
    ("支持", "支援"),
    ("保存", "儲存"),
    ("導入", "匯入"),
    ("導出", "匯出"),
    ("刷新", "重新整理"),
    ("新建", "新增"),
    ("打開", "開啟"),
    ("搜索", "搜尋"),
    ("查找", "尋找"),
    ("調試", "偵錯"),
    ("緩存", "快取"),
    ("設置", "設定"),
    ("會話", "工作階段"),
    ("字符", "字元"),
    ("字節", "位元組"),
    ("界面", "介面"),
    ("操作系統", "作業系統"),
    ("磁盤", "磁碟"),
    ("隊列", "佇列"),
    ("模板", "範本"),
    ("插件", "外掛"),
    ("文本", "文字"),
    ("通過", "透過"),
    ("超時", "逾時"),
    ("訪問", "存取"),
    ("解壓", "解壓縮"),
    ("工具欄", "工具列"),
    ("側欄", "側邊欄"),
    ("狀態欄", "狀態列"),
    ("標題欄", "標題列"),
    ("底欄", "底部列"),
    ("頂欄", "頂部列"),
    ("快捷鍵", "快速鍵"),
    ("消息", "訊息"),
    ("口令", "密碼"),
    ("回車", "Enter"),
    ("佈局", "版面"),
    ("配置檔案", "設定檔"),
    ("配置", "設定"),
    ("當前", "目前"),
    ("本地", "本機"),
    ("地址", "位址"),
    ("令牌", "權杖"),
    ("服務商", "服務供應商"),
    ("客戶端", "用戶端"),
    ("許可權", "權限"),
    ("映象", "映像"),
    ("匹配", "符合"),
    ("分組", "群組"),
    ("自定義", "自訂"),
    ("列表", "清單"),
    ("補全", "自動完成"),
    ("快捷命令", "快速指令"),
    ("命令", "指令"),
    ("二維碼", "QR Code"),
    ("審批", "核准"),
    ("憑據", "認證資訊"),
    ("型別", "類型"),
    ("自動填充", "自動填入"),
    ("填充", "填入"),
    ("臺", "台"),
    ("複用", "重複使用"),
    ("手工", "手動"),
    ("獲取", "取得"),
    ("拖動", "拖曳"),
    ("選中", "選取"),
    ("併發", "並行"),
    ("校驗", "驗證"),
    ("反饋", "回饋"),
    ("響應", "回應"),
    ("重連", "重新連線"),
    ("斷開", "中斷"),
    ("單個", "單一"),
    ("復制", "複製"),
    ("剪切", "剪下"),
    # 標籤 is both a tab and a tag in the source; these are the tabs.
    ("標籤欄", "分頁列"),
    ("標籤方向", "分頁方向"),
    ("工作階段標籤", "工作階段分頁"),
    ("垂直標籤", "垂直分頁"),
    ("新開標籤", "開新分頁"),
    ("單一標籤", "單一分頁"),
    ("透過標籤標題", "透過分頁標題"),
    ("此標籤僅在本機", "此分頁僅在本機"),
    ("目前標籤", "目前分頁"),
    ("全部標籤", "全部分頁"),
    ("頁籤", "分頁"),
    # OpenCC slips and the remaining mainland words.
    ("副本里", "副本裡"),
    ("隻", "只"),
    ("高階", "進階"),
    ("註釋", "註解"),
    ("報錯", "錯誤"),
    ("鑰匙串", "鑰匙圈"),
    ("崩潰", "當機"),
    ("後台", "背景"),
    ("托盤", "系統匣"),
    ("開機自啟", "開機時啟動"),
    ("金鑰鏈", "金鑰"),
    ("“", "「"),
    ("”", "」"),
    ("‘", "『"),
    ("’", "』"),
]
# Each replaces with a guard, so running over text already in the target form is safe.
PATTERNS = [
    (re.compile(r"(?<!連接)埠"), "連接埠"),
    (re.compile(r"終端(?!機)"), "終端機"),
    (re.compile(r"解壓縮縮"), "解壓縮"),
    (re.compile(r"(?<=[一-鿿])QR Code"), " QR Code"),
    (re.compile(r"QR Code(?=[一-鿿])"), "QR Code "),
    # The source mixes half-width punctuation into its sentences; a comment or
    # identifier never has a Han character before it, so code is left alone.
    (re.compile(r"(?<=[一-鿿]),"), "，"),
    (re.compile(r"(?<=[一-鿿]);"), "；"),
    (re.compile(r"(?<=[一-鿿]):"), "："),
    (re.compile(r"(?<=[一-鿿])\?"), "？"),
]
# Language names are written the way their own readers write them.
AFTER = [
    ("覆制", "複製"),
    ("在檔案中查詢", "在檔案中尋找"),
    ('"查詢"', '"尋找"'),
    ("關閉查詢", "關閉尋找"),
    ("替換", "取代"),
    ("跳轉到行", "跳至行"),
    ("製表符", "定位字元"),
    ("生成", "產生"),
    ("重定向", "重新導向"),
    ("簡體中文", "简体中文"),
    # Words s2twp converts before TERMS can see them, or keeps from the
    # mainland wording; spelled out so a regeneration stays stable.
    ("未透過", "未通過"),
    ("指令列", "命令列"),
    ("自動查詢", "自動尋找"),
    ("符合正則", "符合正規表示式"),
    ("的正則", "的正規表示式"),
    ("該正則", "該正規表示式"),
    ("正則太長", "正規表示式太長"),
    ("示例", "範例"),
    ("身份", "身分"),
    ("渠道", "管道"),
]


def source():
    text = (I18N / "chinese.go").read_text(encoding="utf-8")
    body = text[text.index("var ChineseMessages"):]
    blocks = [body.rstrip()]
    for name in SIDE_CATALOGS:
        side = (I18N / name).read_text(encoding="utf-8")
        match = re.search(r"^var chinese\w+ = .*?^}", side, re.M | re.S)
        if match is None:
            raise SystemExit(f"no chinese catalog in {name}")
        blocks.append(match.group(0))
    return "\n\n".join(blocks)


def convert(text):
    text = opencc.OpenCC("s2twp").convert(text)
    for old, new in TERMS:
        text = text.replace(old, new)
    for pattern, new in PATTERNS:
        text = pattern.sub(new, text)
    for old, new in AFTER:
        text = text.replace(old, new)
    text = re.sub(r"\bchinese([A-Z])", r"traditional\1", text)
    return text.replace("ChineseMessages", "TraditionalMessages")


def main():
    header = (
        "// Code generated by tools/zhtw/gen.py from the Simplified Chinese catalog. DO NOT EDIT.\n\n"
        "package i18n\n\n"
    )
    OUTPUT.write_text(header + convert(source()) + "\n", encoding="utf-8", newline="\n")


if __name__ == "__main__":
    main()
