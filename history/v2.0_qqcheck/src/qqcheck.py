#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
qqcheck — QQ 本地数据只读查询工具 v1.0  (2026-09-11)
=====================================================
仅限本人 / 已授权设备与账号使用。
全程本地只读：不联网、不修改 QQ 数据、不上传任何内容。

命令:
  qqcheck check  <号码>                综合体检（自动判定 群/用户）
  qqcheck group  <群号>                群报告（是否在群 + 证据 + 成员统计）
  qqcheck user   <QQ号>                用户报告（映射 + 互动痕迹 + 所在群角色）
  qqcheck member <群号> <QQ号|uid>     成员角色（群主/管理员/头衔）
  qqcheck uid    <QQ号>                号码 -> 内部UID 映射
  qqcheck search <关键词>              关键位置全文搜索（含上下文）
  qqcheck overview                     账号总览

选项:
  --json               JSON 输出（check/group/user/member/uid/overview）
  --space main|dual    限定空间
"""
import os, re, sys, glob, json, time, sqlite3, functools

VERSION = '1.0'
ROOT_MAIN = '/data/data/com.tencent.mobileqq'
TMP_DIR = '/data/local/tmp/qqcheck/tmp'
RE_U = rb'u_[A-Za-z0-9_\-]{10,44}'

# ---------------- 基础工具 ----------------
def die(msg, code=2):
    print('[错误] ' + msg)
    sys.exit(code)

def rd(path):
    try:
        with open(path, 'rb') as f:
            return f.read()
    except Exception:
        return b''

@functools.lru_cache(maxsize=96)
def _rd_c(path, m, s):
    return rd(path)

def rdc(path):
    """带缓存读取：同一进程内同一文件只读一次"""
    try:
        st = os.stat(path)
        return _rd_c(path, st.st_mtime, st.st_size)
    except Exception:
        return b''

def mtime_str(path):
    try:
        return time.strftime('%Y-%m-%d %H:%M', time.localtime(os.path.getmtime(path)))
    except Exception:
        return '?'

def ts(v):
    """时间戳（自动区分秒/毫秒）-> 可读时间"""
    try:
        v = int(v)
    except Exception:
        return str(v)
    if v <= 0:
        return '0'
    if v > 10**12:
        v //= 1000
    if not (10**9 <= v <= 5 * 10**9):
        return str(v)
    return time.strftime('%Y-%m-%d %H:%M:%S', time.localtime(v))

def ctxs(data, token, before=60, after=110, limit=4):
    """取匹配上下文"""
    out, s = [], 0
    tok = token if isinstance(token, bytes) else token.encode()
    while len(out) < limit:
        i = data.find(tok, s)
        if i < 0:
            break
        out.append(data[max(0, i - before): i + len(tok) + after].decode('utf-8', 'ignore').replace('\n', ' '))
        s = i + 1
    return out

# ---------------- 空间 ----------------
def detect_spaces():
    out = []
    if os.path.isdir(ROOT_MAIN):
        out.append(('主空间', ROOT_MAIN))
    for p in sorted(glob.glob('/data/user/*/com.tencent.mobileqq')):
        m = re.search(r'/data/user/(\d+)/', p)
        if m and m.group(1) != '0' and os.path.isdir(p):
            out.append(('分身(u%s)' % m.group(1), p))
    return out

def check_root():
    try:
        os.listdir(ROOT_MAIN)
    except (PermissionError, FileNotFoundError):
        die('无法读取 QQ 数据目录（需要 root）。请用: su -c "qqcheck ..."', 3)

SPACES = []

# ---------------- 数据读取 ----------------
def uid_map(sp):
    """QQ号 -> NT内部UID 映射"""
    data = rdc(sp[1] + '/files/mmkv/qq_uin_uid_map')
    mp = {}
    for mm in re.finditer(rb'uid_prefix_key_(\d{5,12})', data):
        uin = mm.group(1).decode()
        win = data[mm.end(): mm.end() + 95].split(b'uid_prefix_key_')[0]
        um = re.search(RE_U, win)
        if um:
            mp.setdefault(uin, um.group(0).decode())
    return mp

def rev_all():
    """uid -> QQ号（跨全部空间）"""
    mp = {}
    for sp in SPACES:
        for k, v in uid_map(sp).items():
            mp.setdefault(v, k)
    return mp

def title_entries(sp):
    """成员头衔缓存 -> [(群号, uid, 头衔), ...]"""
    data = rdc(sp[1] + '/files/mmkv/common_mmkv_configurations')
    out = []
    for mm in re.finditer(rb'"(\d{5,12})-(u_[A-Za-z0-9_\-]{10,44})":\{[^}]*\}', data):
        tm = re.search(rb'"c":"([^"]*)"', mm.group(0))
        out.append((mm.group(1).decode(), mm.group(2).decode(),
                    tm.group(1).decode('utf-8', 'ignore').strip() if tm else ''))
    return out

def account_numbers(sp):
    nums = set()
    for f in glob.glob(sp[1] + '/databases/*.db'):
        b = os.path.basename(f)[:-3]
        if b.isdigit():
            nums.add(b)
    for f in glob.glob(sp[1] + '/shared_prefs/*.xml'):
        b = os.path.basename(f)[:-4]
        if b.isdigit():
            nums.add(b)
    return sorted(nums)

def nums_all():
    s = set()
    for sp in SPACES:
        s.update(account_numbers(sp))
    return s

# ---------------- 全位置扫描 ----------------
def scan_number(sp, token):
    tok = token if isinstance(token, bytes) else token.encode()
    res = {'prefs': [], 'mmkv': [], 'db': []}
    for f in glob.glob(sp[1] + '/shared_prefs/*'):
        if os.path.isfile(f):
            d = rdc(f)
            if tok in d:
                res['prefs'].append([os.path.basename(f), d.count(tok), mtime_str(f)])
    for f in glob.glob(sp[1] + '/files/mmkv/*'):
        if not os.path.isfile(f) or f.endswith('.crc'):
            continue
        try:
            if os.path.getsize(f) > 96 * 1024 * 1024:
                continue
        except Exception:
            continue
        d = rdc(f)
        if tok in d:
            res['mmkv'].append([os.path.basename(f), d.count(tok), mtime_str(f)])
    for f in glob.glob(sp[1] + '/databases/*.db') + glob.glob(sp[1] + '/databases/*.db-wal'):
        if os.path.isfile(f):
            d = rdc(f)
            if tok in d:
                res['db'].append([os.path.basename(f), d.count(tok), mtime_str(f)])
    return res

# ---------------- 旧版明文库（SQLite） ----------------
def open_legacy_db(sp):
    """打开空间内 <uin>.db 旧版明文库（先拷贝副本再读，不锁原库）"""
    cands = [f for f in glob.glob(sp[1] + '/databases/*.db') if os.path.basename(f)[:-3].isdigit()]
    if not cands:
        return None
    src = max(cands, key=os.path.getsize)
    os.makedirs(TMP_DIR, exist_ok=True)
    cp = TMP_DIR + '/' + os.path.basename(src)
    need = True
    try:
        if os.path.getsize(cp) == os.path.getsize(src) and os.path.getmtime(cp) >= os.path.getmtime(src):
            need = False
    except Exception:
        pass
    if need:
        for suf in ('', '-wal', '-shm'):
            if os.path.exists(src + suf):
                try:
                    with open(src + suf, 'rb') as a, open(cp + suf, 'wb') as b:
                        b.write(a.read())
                except Exception:
                    pass
    try:
        con = sqlite3.connect('file:%s?mode=ro' % cp, uri=True)
        con.execute('SELECT 1')
        return con
    except Exception:
        return None

def qt(con, sql, args=()):
    try:
        return con.execute(sql, args).fetchall()
    except Exception:
        return []

def has_table(con, t):
    return bool(qt(con, "SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", (t,)))

def group_evidence_db(con, g):
    """旧版库中该群的证据汇总"""
    ev = {}
    if con is None:
        return ev
    t = 'TroopFileTansferItemEntity' + g
    if has_table(con, t):
        rows = qt(con, 'SELECT UploadTime FROM "%s"' % t)
        times = [r[0] for r in rows if isinstance(r[0], int) and r[0] > 10**9]
        ev['file_table'] = [len(rows), ts(min(times)) if times else '?', ts(max(times)) if times else '?']
    rows = qt(con, "SELECT srvTime FROM mr_fileManager WHERE CAST(TroopUin AS TEXT)=?", (g,))
    if rows:
        times = [r[0] for r in rows if isinstance(r[0], int) and r[0] > 10**9]
        ev['file_mgr'] = [len(rows), ts(min(times)) if times else '?', ts(max(times)) if times else '?']
    rows = qt(con, "SELECT time FROM TroopNotificationCache WHERE CAST(troopUin AS TEXT)=?", (g,))
    if rows:
        times = [r[0] for r in rows if isinstance(r[0], int) and r[0] > 10**9]
        ev['notif'] = [len(rows), ts(max(times)) if times else '?']
    rows = qt(con, "SELECT opTime FROM TroopEssenceMsgItem WHERE CAST(troopUin AS TEXT)=?", (g,))
    if rows:
        times = [r[0] for r in rows if isinstance(r[0], int) and r[0] > 10**9]
        ev['essence'] = [len(rows), ts(max(times)) if times else '?']
    return ev

def groups_with_files(con):
    if con is None:
        return []
    return [n.replace('TroopFileTansferItemEntity', '')
            for (n,) in qt(con, "SELECT name FROM sqlite_master WHERE name LIKE 'TroopFileTansferItemEntity%'")]

def detect_kind(tok):
    """自动判定号码是群还是用户"""
    gs, us = [], []
    tb = re.compile(rb'"' + re.escape(tok.encode()) + rb'-u_[A-Za-z0-9_\-]{6,44}"')
    for sp in SPACES:
        d = rdc(sp[1] + '/files/mmkv/common_mmkv_configurations')
        if tb.search(d):
            gs.append('%s：成员头衔缓存含 "<群号>-uid" 键' % sp[0])
        for f in glob.glob(sp[1] + '/databases/*.db'):
            if b'TroopFileTansferItemEntity' + tok.encode() in rdc(f):
                gs.append('%s：存在该群的群文件记录表' % sp[0])
                break
        um = uid_map(sp)
        if tok in um:
            us.append('%s：号码→UID 映射（%s）' % (sp[0], um[tok]))
            uidv = um[tok].encode()
            for f in glob.glob(sp[1] + '/files/mmkv/q_chat_item_mmkv_file_*'):
                if uidv in rdc(f):
                    us.append('%s：聊天列表出现（uid 形式）' % sp[0])
                    break
        if b'QQMutualMark_' + tok.encode() + b'_' in d:
            us.append('%s：好友互动标识痕迹' % sp[0])
    return gs, us

# ---------------- 报告：群 ----------------
def report_group(g):
    g = str(g)
    res = {'group': g, 'generated': time.strftime('%Y-%m-%d %H:%M:%S'), 'spaces': []}
    rev = rev_all()
    merged = {}
    for sp in SPACES:
        for gg, uu, tt in title_entries(sp):
            if gg == g:
                merged[uu] = tt
    for sp in SPACES:
        r = {'space': sp[0], 'accounts': [], 'member_count': 0}
        nums = [n for n in account_numbers(sp) if len(n) >= 6]
        con = open_legacy_db(sp)
        ev = group_evidence_db(con, g)
        for acc in nums:
            a = {'account': acc, 'evidence': [], 'score': 0, 'verdict': ''}
            if 'file_table' in ev:
                n, t1, t2 = ev['file_table']
                a['evidence'].append(['群文件记录表：%s条（%s → %s）' % (n, t1, t2), 3])
            if 'file_mgr' in ev:
                n, t1, t2 = ev['file_mgr']
                a['evidence'].append(['群文件管理：%s条（%s → %s）' % (n, t1, t2), 2])
            if 'notif' in ev:
                a['evidence'].append(['群通知缓存：%s条（最新 %s）' % (ev['notif'][0], ev['notif'][1]), 1])
            if 'essence' in ev:
                a['evidence'].append(['群精华消息：%s条（最新 %s）' % (ev['essence'][0], ev['essence'][1]), 1])
            pf = sp[1] + '/shared_prefs/%s_%s.xml' % (acc, g)
            if os.path.exists(pf):
                a['evidence'].append(['账号×群专属配置：存在（改于 %s）' % mtime_str(pf), 3])
            cnt, last = 0, ''
            for f in glob.glob(sp[1] + '/shared_prefs/troop_gt_af_daily_%s_*.xml' % acc):
                if g.encode() in rdc(f):
                    cnt += 1
                    last = max(last, os.path.basename(f))
            if cnt:
                a['evidence'].append(['群每日记录：%d 天提及（最新 %s）' % (cnt, last[-14:-4] if len(last) > 14 else last), 2])
            d = rdc(sp[1] + '/shared_prefs/last_update_time%s.xml' % acc)
            m = re.search(rb'key_last_update_time' + g.encode() + rb'" value="(\d+)"', d)
            if m:
                a['evidence'].append(['群最后更新：%s' % ts(m.group(1)), 1])
            qc = sp[1] + '/files/mmkv/q_chat_item_mmkv_file_%s' % acc
            if os.path.exists(qc) and g.encode() in rdc(qc):
                a['evidence'].append(['聊天列表：在列（文件更新于 %s）' % mtime_str(qc), 3])
            a['score'] = sum(e[1] for e in a['evidence'])
            a['verdict'] = ('已加入该群（强证据）' if a['score'] >= 6 else
                            '在该群（中等证据）' if a['score'] >= 3 else
                            '仅弱痕迹' if a['score'] > 0 else '未发现证据')
            r['accounts'].append(a)
        mem = {}
        for gg, uu, tt in title_entries(sp):
            if gg == g:
                mem[uu] = tt
        r['member_count'] = len(mem)
        if mem:
            from collections import Counter
            r['roles'] = dict(Counter(v if v else '(无头衔)' for v in mem.values()).most_common(12))
            r['owner'] = [(u, rev.get(u, '?')) for u, v in mem.items() if '群主' in v]
            r['admins'] = [(u, rev.get(u, '?')) for u, v in mem.items() if v == '管理员']
            na = nums_all()
            r['self_titles'] = {rev[u]: v for u, v in mem.items() if u in rev and rev[u] in na}
        res['spaces'].append(r)
    res['member_total'] = len(merged)
    return res

def render_group(res):
    print('=' * 60)
    print(' 群报告：%s' % res['group'])
    print('=' * 60)
    for r in res['spaces']:
        print()
        print('【%s】' % r['space'])
        for a in r['accounts']:
            print('  账号 %s  →  %s' % (a['account'], a['verdict']))
            for txt, w in a['evidence']:
                print('      · %s' % txt)
            if not a['evidence']:
                print('      · （无证据）')
        if r.get('member_count'):
            print('  成员头衔缓存：%d 人' % r['member_count'])
            print('    角色分布：' + '；'.join('%s×%d' % (k, v) for k, v in list(r.get('roles', {}).items())[:8]))
            if r.get('owner'):
                print('    群主：' + '，'.join('%s(%s)' % (u, n) for u, n in r['owner']))
            if r.get('admins'):
                print('    管理员：' + '，'.join('%s(%s)' % (u, n) for u, n in r['admins'][:6]))
            if r.get('self_titles'):
                print('    本机账号头衔：' + '；'.join('%s=%s' % (k, v) for k, v in r['self_titles'].items()))
    print()
    print('（本地只读分析；权威状态以 QQ 内实际显示为准）')

# ---------------- 报告：用户 ----------------
def report_user(uin):
    uin = str(uin)
    res = {'user': uin, 'generated': time.strftime('%Y-%m-%d %H:%M:%S'), 'spaces': []}
    allt = []
    for sp in SPACES:
        for gg, uu, tt in title_entries(sp):
            allt.append((gg, uu, tt))
    for sp in SPACES:
        r = {'space': sp[0], 'uid': None, 'groups': [], 'marks': [], 'signals': [], 'files': {}}
        um = uid_map(sp)
        uid = um.get(uin)
        r['uid'] = uid
        key = uid or uin
        if uid:
            seen = set()
            for gg, uu, tt in allt:
                if uu == uid and gg not in seen:
                    seen.add(gg)
                    r['groups'].append({'group': gg, 'title': tt or '(无头衔)'})
        d = rdc(sp[1] + '/files/mmkv/common_mmkv_configurations')
        for mm in re.finditer(rb'QQMutualMark_' + re.escape(uin.encode()) + b'_?([^_\x00"]{2,80})_([0-9]{5,12})', d):
            mk = mm.group(1).decode('utf-8', 'ignore')
            if mk not in [x[0] for x in r['marks']]:
                r['marks'].append([mk, mm.group(2).decode()])
        for acc in account_numbers(sp):
            pf = sp[1] + '/shared_prefs/pai_yi_pai_user_double_tap_timestamp_%s.xml' % acc
            if not os.path.exists(pf):
                continue
            pd = rdc(pf)
            for k in {uin, key}:
                m = re.search(rb'name="' + k.encode() + rb'" value="(\d+)"', pd)
                if m:
                    r['signals'].append('拍一拍：%s' % ts(int(m.group(1))))
        d2 = rdc(sp[1] + '/shared_prefs/com.tencent.mobileqq_preferences.xml')
        for mm in re.finditer(rb'<string name="special_sound\d+">([^<]*)</string>', d2):
            if re.search(rb'(?<!\d)' + uin.encode() + rb'(?!\d)', mm.group(1)):
                r['signals'].append('特别关心/特殊提示音名单：在列')
                break
        mm = re.search(rb'spcares[^0-9;\x00<]{0,6}([0-9;]{10,400})', d)
        if mm and uin.encode() in mm.group(1).split(b';'):
            r['signals'].append('spcares(特别关心)名单：在列')
        for f in glob.glob(sp[1] + '/files/mmkv/q_chat_item_mmkv_file_*'):
            qd = rdc(f)
            if key.encode() in qd or uin.encode() in qd:
                r['signals'].append('聊天列表：在列（%s）' % os.path.basename(f))
                break
        sc = scan_number(sp, uin)
        r['files'] = {'prefs': sc['prefs'][:10], 'mmkv': sc['mmkv'][:12], 'db': sc['db'][:8]}
        res['spaces'].append(r)
    return res

def render_user(res):
    print('=' * 60)
    print(' 用户报告：%s' % res['user'])
    print('=' * 60)
    anyg = False
    for r in res['spaces']:
        print()
        print('【%s】%s' % (r['space'], ('UID: ' + r['uid']) if r['uid'] else '号码未映射到 UID'))
        if r['groups']:
            anyg = True
            print('  群成员角色：')
            for g in r['groups']:
                print('    · 群 %s → %s' % (g['group'], g['title']))
        if r['marks']:
            print('  互动标识：' + '、'.join(m[0] for m in r['marks']))
        for s in r['signals']:
            print('  · %s' % s)
        f = r.get('files', {})
        n1, n2, n3 = len(f.get('prefs', [])), len(f.get('mmkv', [])), len(f.get('db', []))
        if n1 + n2 + n3:
            print('  文件命中：prefs %d 个 / mmkv %d 个 / 数据库 %d 个' % (n1, n2, n3))
            for name, n, mt in f.get('mmkv', [])[:6]:
                print('      · [mmkv] %s（%d处, %s）' % (name, n, mt))
    if not anyg:
        print()
        print('（未发现该用户出现在"群成员头衔缓存"中）')
    print()
    print('（本地只读分析；"是否好友"以互动痕迹推断，权威状态以 QQ 内为准）')

# ---------------- 报告：成员角色 ----------------
def report_member(g, key):
    g = str(g)
    res = {'group': g, 'key': key, 'uid': None, 'uin': None, 'title': None,
           'entries': 0, 'raw': [], 'controls': {}}
    rev = rev_all()
    uid = None
    if key.startswith('u_'):
        uid = key
        res['uid'] = uid
        res['uin'] = rev.get(uid)
    else:
        for sp in SPACES:
            um = uid_map(sp)
            if key in um:
                uid = um[key]
                res['uid'] = uid
                res['uin'] = key
                break
    if not uid:
        res['error'] = '未找到该号码的 UID 映射（可能未出现在缓存中）'
        return res
    titles = []
    for sp in SPACES:
        d = rdc(sp[1] + '/files/mmkv/common_mmkv_configurations')
        pat = b'"%s-%s"' % (g.encode(), uid.encode())
        res['raw'].append([sp[0], d.count(pat)])
    for sp in SPACES:
        for gg, uu, tt in title_entries(sp):
            if gg == g and uu == uid:
                titles.append(tt)
    res['entries'] = len(titles)
    if titles:
        res['title'] = titles[-1] or '(空头衔)'
    mem = {}
    for sp in SPACES:
        for gg, uu, tt in title_entries(sp):
            if gg == g:
                mem[uu] = tt
    if mem:
        from collections import Counter
        res['controls']['members'] = len(mem)
        res['controls']['roles'] = dict(Counter(v if v else '(无)' for v in mem.values()).most_common(8))
        res['controls']['owner'] = [u for u, v in mem.items() if '群主' in v]
        res['controls']['admins'] = [u for u, v in mem.items() if v == '管理员']
    return res

def render_member(res):
    print('=' * 60)
    print(' 成员角色查询：群 %s × %s' % (res['group'], res['key']))
    print('=' * 60)
    if res.get('error'):
        print('[!] %s' % res['error'])
        return
    print('  UID: %s%s' % (res['uid'], ('（QQ号 %s）' % res['uin']) if res.get('uin') else ''))
    if res['title'] is not None:
        print('  头衔条目："c":"%s"（命中 %d 次）' % (res['title'], res['entries']))
    else:
        print('  未在成员头衔缓存中找到该成员（可能不在群/未缓存）')
    c = res.get('controls', {})
    if c:
        print('  同群对照：共 %d 人缓存；角色分布：%s' % (c.get('members', 0),
              '；'.join('%s×%d' % (k, v) for k, v in list(c.get('roles', {}).items())[:6])))
        if c.get('owner'):
            print('    群主：' + '，'.join(c['owner']))
        if c.get('admins'):
            print('    管理员（%d）：%s' % (len(c['admins']), '，'.join(c['admins'][:8])))
    if res['title']:
        print('  ★ 判定：该成员在群 %s 的角色/头衔为【%s】' % (res['group'], res['title']))
    print()
    print('（本地只读分析；权威状态以 QQ 内为准）')

# ---------------- 报告：总览 ----------------
def report_overview():
    res = []
    for sp in SPACES:
        r = {'space': sp[0], 'accounts': account_numbers(sp), 'spcares': [],
             'groups_with_files': [], 'freshness': {}}
        d = rdc(sp[1] + '/files/mmkv/common_mmkv_configurations')
        mm = re.search(rb'spcares[^0-9;\x00<]{0,6}([0-9;]{10,400})', d)
        if mm:
            r['spcares'] = [x.decode() for x in mm.group(1).split(b';') if x]
        con = open_legacy_db(sp)
        if con:
            r['groups_with_files'] = groups_with_files(con)
        for k in ['shared_prefs', 'files/mmkv', 'databases']:
            r['freshness'][k] = mtime_str(sp[1] + '/' + k)
        res.append(r)
    return res

def render_overview(res):
    print('=' * 60)
    print(' 账号总览')
    print('=' * 60)
    for r in res:
        print()
        print('【%s】' % r['space'])
        print('  账号：' + ('、'.join(r['accounts'][:8]) if r['accounts'] else '?'))
        if r['spcares']:
            print('  spcares(特别关心)名单：%s' % '、'.join(r['spcares'][:12]))
        if r['groups_with_files']:
            print('  有文件记录的群（%d）：%s' % (len(r['groups_with_files']), '、'.join(r['groups_with_files'][:15])))
        for k, v in r['freshness'].items():
            print('  %s 目录改动于：%s' % (k, v))

# ---------------- 入口 ----------------
def usage():
    print(__doc__)

def norm_number(x):
    if not re.fullmatch(r'\d{5,12}', x):
        die('号码格式不正确（应为 5~12 位数字）: %s' % x)
    return x

def main():
    global SPACES
    args = sys.argv[1:]
    if not args or args[0] in ('-h', '--help', 'help'):
        usage()
        return
    as_json = False
    space_filter = 'all'
    rest = []
    i = 0
    while i < len(args):
        a = args[i]
        if a == '--json':
            as_json = True
            i += 1
        elif a == '--space':
            space_filter = args[i + 1] if i + 1 < len(args) else 'all'
            i += 2
        elif a.startswith('--space='):
            space_filter = a.split('=', 1)[1]
            i += 1
        elif a in ('-v', '--version'):
            print('qqcheck v' + VERSION)
            return
        else:
            rest.append(a)
            i += 1
    SPACES = detect_spaces()
    if space_filter == 'main':
        SPACES = [s for s in SPACES if s[0] == '主空间']
    elif space_filter == 'dual':
        SPACES = [s for s in SPACES if s[0] != '主空间']
    if not SPACES:
        die('未发现 QQ 数据目录')
    check_root()
    cmd = rest[0] if rest else 'help'

    def emit(obj, renderer):
        if as_json:
            print(json.dumps(obj, ensure_ascii=False, indent=2))
        else:
            renderer(obj)

    if cmd == 'check':
        if len(rest) < 2:
            die('用法: qqcheck check <号码>')
        tok = norm_number(rest[1])
        gs, us = detect_kind(tok)
        if gs and not us:
            print('判定：该号码是【群】（依据：%s）' % '；'.join(gs[:2]))
            emit(report_group(tok), render_group)
        elif us and not gs:
            print('判定：该号码是【用户】（依据：%s）' % '；'.join(us[:2]))
            emit(report_user(tok), render_user)
        elif gs and us:
            print('提示：同时发现群与用户痕迹，分别输出两份报告')
            emit(report_group(tok), render_group)
            emit(report_user(tok), render_user)
        else:
            print('未在本地数据中发现该号码的痕迹。可尝试: qqcheck search %s' % tok)
    elif cmd == 'group':
        if len(rest) < 2:
            die('用法: qqcheck group <群号>')
        emit(report_group(norm_number(rest[1])), render_group)
    elif cmd == 'user':
        if len(rest) < 2:
            die('用法: qqcheck user <QQ号>')
        emit(report_user(norm_number(rest[1])), render_user)
    elif cmd == 'member':
        if len(rest) < 3:
            die('用法: qqcheck member <群号> <QQ号|uid>')
        g = norm_number(rest[1])
        key = rest[2]
        if not re.fullmatch(r'\d{5,12}|u_[A-Za-z0-9_\-]+', key):
            die('第二个参数应为 QQ号 或 uid（u_开头）')
        emit(report_member(g, key), render_member)
    elif cmd == 'uid':
        if len(rest) < 2:
            die('用法: qqcheck uid <QQ号>')
        uin = norm_number(rest[1])
        out = {'qq': uin, 'maps': []}
        for sp in SPACES:
            um = uid_map(sp)
            if uin in um:
                out['maps'].append({'space': sp[0], 'uid': um[uin]})
        if as_json:
            print(json.dumps(out, ensure_ascii=False, indent=2))
        else:
            print('QQ号 %s 的 NT内部UID:' % uin)
            for m in out['maps']:
                print('  【%s】%s' % (m['space'], m['uid']))
            if not out['maps']:
                print('  （未找到映射；该号码可能未出现在本地缓存中）')
    elif cmd == 'search':
        if len(rest) < 2:
            die('用法: qqcheck search <关键词或号码>')
        kw = rest[1]
        for sp in SPACES:
            print('【%s】搜索: %s' % (sp[0], kw))
            sc = scan_number(sp, kw)
            any_hit = False
            for cat, label, sub in [('prefs', 'shared_prefs', '/shared_prefs/'),
                                    ('mmkv', 'mmkv', '/files/mmkv/'),
                                    ('db', 'databases', '/databases/')]:
                for name, n, mt in sc[cat]:
                    any_hit = True
                    print('  [%s] %s  (%d处, %s)' % (label, name, n, mt))
                    for c in ctxs(rdc(sp[1] + sub + name), kw, limit=2):
                        print('      …%s…' % c)
            if not any_hit:
                print('  （无命中）')
        print()
        print('提醒：必须核对上下文，防止子串假阳性（如 blackfriday / sig_feed_id 前缀）。')
    elif cmd == 'overview':
        emit(report_overview(), render_overview)
    else:
        print('未知命令: %s' % cmd)
        usage()

if __name__ == '__main__':
    main()
