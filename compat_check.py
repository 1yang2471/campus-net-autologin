# -*- coding: utf-8 -*-
"""门户兼容性自检（离线，不联网、不发凭据）。

两种模式：
- 默认（响应判定）：抓一次登录响应（浏览器 F12 / 抓包），看会被判成
  「成功 / 拒绝 / 瞬态」以及命中哪条规则。
    python compat_check.py "{'success':true,'msg':'ok'}"
    python compat_check.py --file response.txt
- --detect（协议指纹）：抓登录页 HTML / 登录 POST 抓包，判断属不属于
  ac_portal 族、是不是需要 auth_tag / 密码加密的变体，还是别家协议。
    python compat_check.py --detect login_page.html
    python compat_check.py --detect --file page.html

判定不符时，在 %LOCALAPPDATA%\\campus-net-autologin\\config.ini
加 [response] 段覆盖词表（见 README「异校适配」），再跑一次验证。
"""
from __future__ import annotations

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from keeper import compat_check, load_config, protocol_fingerprint  # noqa: E402

_VERDICT_LABELS = {
    "compatible": "compatible（朴素 ac_portal，本工具适用）",
    "variant_unsupported": "variant_unsupported（ac_portal 变体：auth_tag/密码加密，本工具暂不支持）",
    "other_protocol": "other_protocol（非 ac_portal 门户，本工具不适用）",
    "unknown": "unknown（未识别出已知门户特征）",
}


def main() -> None:
    args = sys.argv[1:]
    detect = "--detect" in args
    if detect:
        args.remove("--detect")
    if not args:
        print(__doc__)
        raise SystemExit(2)
    if args[0] == "--file":
        if len(args) < 2:
            print("--file 需要一个样本文件路径", file=sys.stderr)
            raise SystemExit(2)
        sample = Path(args[1]).read_text(encoding="utf-8", errors="replace")
    else:
        sample = args[0]

    if detect:
        r = protocol_fingerprint(sample)
        print(f"协议族: {r['family']}")
        print(f"结论: {_VERDICT_LABELS.get(r['verdict'], r['verdict'])}")
        print(f"auth_tag: {'有（提交前须从页面取一次性令牌）' if r['needs_auth_tag'] else '无'}")
        print(f"密码加密标记: {'疑似命中' if r['pwd_encrypted'] else '无'}")
        print(f"建议: {r['advice']}")
        return

    r = compat_check(sample, load_config())
    print(f"判定: {r['verdict']}")
    print(f"规则: {r['rule']}")
    print(f"细节: {r['detail'] or '(无)'}")
    print(f"瞬态(可重试): {'是' if r['transient'] else '否'}")
    if r["verdict"] == "reject":
        print("\n若这与门户实际结果不符，请按 README「异校适配」配置 [response] 词表后重试。")


if __name__ == "__main__":
    main()
