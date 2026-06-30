# -*- coding: utf-8 -*-
import os, json, time, threading
from flask import Flask, render_template_string, request, redirect, jsonify
from watchfiles import watch, Change

app = Flask(__name__)

# --- 核心路径配置 ---
BASE_PATH = os.environ.get("BASE_PATH", "/CloudNAS")
CONFIG_PATH = os.environ.get("CONFIG_PATH", "config.json")
CACHE_PATH = os.environ.get("CACHE_PATH", "cache.json")
LOG_PATH = os.environ.get("LOG_PATH", "clean.log")

DEFAULT_CONFIG = {
    "tasks": [],
    "cfg": {
        "ad_exts": ".txt,.html,.url,.lnk",
        "video_exts": ".mp4,.mkv,.ts",
        "size_limit": 5.0,
        "exclude_dirs": "重要,备份",
        "watch_enabled": "on"
    }
}

# --- 基础工具函数 ---
def load_json(path, default):
    if not os.path.exists(path):
        save_json(path, default)
        return default
    try:
        with open(path, 'r', encoding='utf-8') as f:
            data = json.load(f)
            if path == CONFIG_PATH:
                if "cfg" not in data: data["cfg"] = DEFAULT_CONFIG["cfg"]
                if "tasks" not in data: data["tasks"] = []
            return data
    except: return default

def save_json(path, data):
    try:
        with open(path, 'w', encoding='utf-8') as f:
            json.dump(data, f, ensure_ascii=False, indent=4)
        return True
    except: return False

def add_log(msg):
    t = time.strftime('%Y-%m-%d %H:%M:%S')
    try:
        with open(LOG_PATH, 'a', encoding='utf-8') as f:
            f.write(f"[{t}] {msg}\n")
    except: pass

# --- 更新缓存指纹的函数 (新增) ---
def update_dir_cache(dir_path):
    """当监控发现变动时，同步更新该目录的缓存指纹"""
    try:
        if not os.path.exists(dir_path): return
        cache = load_json(CACHE_PATH, {})
        files = os.listdir(dir_path)
        # 生成新指纹：文件数_最后修改时间
        finger = f"{len(files)}_{int(os.stat(dir_path).st_mtime)}"
        cache[dir_path] = finger
        save_json(CACHE_PATH, cache)
    except: pass

# --- 清理逻辑 ---
def execute_clean(file_path):
    conf = load_json(CONFIG_PATH, DEFAULT_CONFIG)
    cfg = conf.get('cfg', DEFAULT_CONFIG['cfg'])
    ads = [x.strip().lower() for x in str(cfg.get('ad_exts','')).split(',') if x.strip()]
    vids = [x.strip().lower() for x in str(cfg.get('video_exts','')).split(',') if x.strip()]
    limit = float(cfg.get('size_limit', 5.0)) * 1024 * 1024
    
    if not os.path.exists(file_path) or os.path.isdir(file_path): return
    
    try:
        fname = os.path.basename(file_path).lower()
        parent_dir = os.path.dirname(file_path)
        is_del = False
        if any(fname.endswith(ex) for ex in ads): is_del = True
        elif any(fname.endswith(v) for v in vids):
            if os.path.getsize(file_path) < limit: is_del = True
            
        if is_del:
            os.remove(file_path)
            add_log(f"🔥 [监控/扫描] 清理: {file_path.replace(BASE_PATH, '')}")
        
        # 无论是否删除，只要该目录发生了事件，就更新其缓存指纹
        update_dir_cache(parent_dir)
    except: pass

# --- 增量扫描引擎 ---
def start_full_scan():
    conf = load_json(CONFIG_PATH, DEFAULT_CONFIG)
    cache = load_json(CACHE_PATH, {})
    add_log("📂 启动全量增量扫描...")
    count, skip = 0, 0
    new_cache = {}
    
    for t in conf.get('tasks', []):
        target = os.path.join(BASE_PATH, t['path'].strip('/'))
        if not os.path.exists(target):
            add_log(f"❌ 路径不存在: {target}")
            continue
        for root, _, files in os.walk(target):
            try:
                finger = f"{len(files)}_{int(os.stat(root).st_mtime)}"
                if cache.get(root) == finger:
                    skip += 1
                    new_cache[root] = finger
                    continue
                for f in files:
                    execute_clean(os.path.join(root, f))
                    count += 1
                new_cache[root] = finger
            except: continue
            
    save_json(CACHE_PATH, new_cache)
    add_log(f"✅ 扫描完成: 处理文件 {count}, 跳过未变动 {skip}")

# --- 监控事件处理 ---
watchdog_enabled = False
file_fingerprints = {}
dir_fingerprints = {}
dir_access_time = {}
MIN_ACCESS_INTERVAL = 60  

def get_file_fingerprint(file_path):
    try:
        if os.path.isfile(file_path):
            stat = os.stat(file_path)
            return f"{stat.st_mtime}_{stat.st_size}"
        return None
    except:
        return None

def get_dir_fingerprint(dir_path):
    try:
        if os.path.isdir(dir_path):
            stat = os.stat(dir_path)
            return f"{stat.st_mtime}_{stat.st_size}"
        return None
    except:
        return None

def get_all_parent_dirs(file_path):
    dirs = []
    current = os.path.dirname(file_path)
    while current != '/' and current != '' and current != BASE_PATH.rstrip('/'):
        dirs.append(current)
        current = os.path.dirname(current)
    return dirs

def handle_file_event(file_path):
    try:
        if not os.path.isfile(file_path):
            return
        
        fingerprint = get_file_fingerprint(file_path)
        if not fingerprint:
            return
        
        if file_path in file_fingerprints and file_fingerprints[file_path] == fingerprint:
            add_log(f"⏭️ 文件未变化: {os.path.basename(file_path)}")
            return
        
        current_time = time.time()
        
        parent_dirs = get_all_parent_dirs(file_path)
        for dir_path in parent_dirs:
            if dir_path in dir_access_time:
                if current_time - dir_access_time[dir_path] < MIN_ACCESS_INTERVAL:
                    add_log(f"⏳ 节流: 父目录 {dir_path} 访问过于频繁")
                    return
        
        file_dir = os.path.dirname(file_path)
        if file_dir in dir_access_time:
            if current_time - dir_access_time[file_dir] < MIN_ACCESS_INTERVAL:
                add_log(f"⏳ 节流: {file_dir} 访问过于频繁")
                return
        
        add_log(f"🔍 处理文件: {file_path}")
        execute_clean(file_path)
        
        file_fingerprints[file_path] = fingerprint
        dir_access_time[file_dir] = current_time
        
        for dir_path in parent_dirs:
            dir_access_time[dir_path] = current_time
        
        cache = load_json(CACHE_PATH, {})
        cache[file_path] = fingerprint
        save_json(CACHE_PATH, cache)
        
    except Exception as ex:
        add_log(f"❌ 处理文件失败: {file_path}, {str(ex)}")

def watchdog_worker():
    global watchdog_enabled, file_fingerprints, dir_fingerprints
    watchdog_enabled = True
    add_log("🚀 监控服务已启动 (多级目录时间模式)")
    add_log(f"ℹ️ 目录访问间隔: {MIN_ACCESS_INTERVAL}秒")
    
    cache = load_json(CACHE_PATH, {})
    for key in cache:
        if os.path.isfile(key):
            file_fingerprints[key] = cache[key]
    add_log(f"ℹ️ 已加载 {len(file_fingerprints)} 个文件指纹")
    
    while watchdog_enabled:
        try:
            d = load_json(CONFIG_PATH, DEFAULT_CONFIG)
            if d['cfg'].get('watch_enabled') != 'on':
                add_log("🔕 监控已禁用，等待启用...")
                time.sleep(15)
                continue
            
            paths_to_watch = []
            for t in d.get('tasks', []):
                target = os.path.join(BASE_PATH, t['path'].lstrip('/'))
                if os.path.exists(target):
                    paths_to_watch.append(target)
            
            if not paths_to_watch:
                add_log("⚠️ 没有可监控的目录，等待配置...")
                time.sleep(15)
                continue
            
            add_log(f"🔔 监控已启用，正在监控 {len(paths_to_watch)} 个目录 (多级目录时间模式)")
            
            try:
                for changes in watch(*paths_to_watch, poll_delay_ms=10000):
                    if not watchdog_enabled:
                        break
                    d = load_json(CONFIG_PATH, DEFAULT_CONFIG)
                    if d['cfg'].get('watch_enabled') != 'on':
                        break
                    
                    for change, path in changes:
                        if change in (Change.added, Change.modified):
                            if os.path.isfile(path):
                                handle_file_event(path)
                            elif os.path.isdir(path):
                                dir_fingerprint = get_dir_fingerprint(path)
                                old_fingerprint = dir_fingerprints.get(path)
                                if dir_fingerprint and dir_fingerprint != old_fingerprint:
                                    add_log(f"📂 目录时间变化: {path}")
                                    dir_fingerprints[path] = dir_fingerprint
                                elif dir_fingerprint:
                                    add_log(f"⏭️ 目录时间未变化: {path}")
                    
                    time.sleep(3)
                    
            except Exception as ex:
                add_log(f"⚠️ 监控循环异常: {str(ex)}")
                time.sleep(15)
                
        except Exception as ex:
            add_log(f"❌ 监控服务异常: {str(ex)}")
            time.sleep(15)
    
    add_log("🛑 监控服务已停止")

# --- Flask 路由 (保持不变) ---
@app.route('/')
def index():
    conf = load_json(CONFIG_PATH, DEFAULT_CONFIG)
    return render_template_string(HTML_TPL, conf=conf)

@app.route('/api/save_cfg', methods=['POST'])
def save_cfg():
    d = load_json(CONFIG_PATH, DEFAULT_CONFIG)
    d['cfg']['ad_exts'] = request.form.get('ad_exts', d['cfg']['ad_exts'])
    d['cfg']['video_exts'] = request.form.get('video_exts', d['cfg']['video_exts'])
    d['cfg']['size_limit'] = request.form.get('size_limit', d['cfg']['size_limit'])
    d['cfg']['exclude_dirs'] = request.form.get('exclude_dirs', d['cfg']['exclude_dirs'])
    d['cfg']['watch_enabled'] = request.form.get('watch_enabled', 'off')
    save_json(CONFIG_PATH, d); return redirect('/')

@app.route('/api/save_conf_json', methods=['POST'])
def save_conf_json():
    try:
        new_data = json.loads(request.form.get('json_content'))
        save_json(CONFIG_PATH, new_data); return jsonify({"success": True})
    except Exception as e: return jsonify({"success": False, "msg": str(e)})

@app.route('/api/get_all_data')
def get_all_data():
    if not os.path.exists(LOG_PATH): logs = ["等待记录..."]
    else:
        with open(LOG_PATH, 'r', encoding='utf-8') as f: logs = f.readlines()[-50:][::-1]
    return jsonify({
        "conf": load_json(CONFIG_PATH, DEFAULT_CONFIG),
        "cache": load_json(CACHE_PATH, {}),
        "logs": logs
    })

@app.route('/api/manual_clean')
def manual_clean():
    threading.Thread(target=start_full_scan, daemon=True).start(); return jsonify({"success": True})

@app.route('/api/clear_cache')
def clear_cache():
    save_json(CACHE_PATH, {}); return jsonify({"success": True})

@app.route('/api/clear_log')
def clear_log():
    try:
        with open(LOG_PATH, 'w', encoding='utf-8') as f:
            f.write('')
        return jsonify({"success": True})
    except:
        return jsonify({"success": False})

@app.route('/api/list_dir')
def list_dir():
    p = request.args.get('path', '').strip('/')
    full = os.path.join(BASE_PATH, p) if p else BASE_PATH
    try:
        dirs = sorted([d for d in os.listdir(full) if os.path.isdir(os.path.join(full, d)) and not d.startswith('.')])
        return jsonify({"success": True, "dirs": dirs})
    except: return jsonify({"success": False})

@app.route('/api/add_task', methods=['POST'])
def add_task():
    d = load_json(CONFIG_PATH, DEFAULT_CONFIG); p = request.form.get('path')
    if p: d['tasks'].append({"id": int(time.time()), "path": p}); save_json(CONFIG_PATH, d)
    return redirect('/')

@app.route('/del_task/<int:tid>')
def del_task(tid):
    d = load_json(CONFIG_PATH, DEFAULT_CONFIG); d['tasks'] = [t for t in d['tasks'] if t['id'] != tid]
    save_json(CONFIG_PATH, d); return redirect('/')

# --- 前端界面 (带分页搜索) ---
HTML_TPL = """
<!DOCTYPE html><html><head><meta charset="UTF-8"><title>netdrive-sweeper</title>
<style>
body{font-family:sans-serif;padding:20px;background:#f5f7fa;font-size:14px;color:#333}
.card{background:#fff;padding:20px;border-radius:10px;margin-bottom:20px;box-shadow:0 4px 12px rgba(0,0,0,0.05)}
input, select, textarea{width:100%;padding:10px;margin:8px 0;box-sizing:border-box;border:1px solid #ddd;border-radius:6px;outline:none}
.btn{background:#409EFF;color:#fff;border:none;padding:10px 20px;border-radius:6px;cursor:pointer;font-weight:bold}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:20px}
.log-area{height:300px;overflow:auto;background:#2d2d2d;color:#a6e22e;padding:15px;font-family:monospace;font-size:12px;border-radius:6px;white-space:pre-wrap;line-height:1.6}
#picker{display:none;position:fixed;top:10%;left:50%;transform:translateX(-50%);width:90%;max-width:450px;background:#fff;border-radius:12px;box-shadow:0 10px 30px rgba(0,0,0,0.2);padding:20px;z-index:1000}
.tag{padding:3px 10px;border-radius:20px;font-size:12px;color:#fff;margin-right:8px}
.collapsible {background-color: #fff; color: #444; cursor: pointer; padding: 15px; width: 100%; border: 1px solid #eee; text-align: left; outline: none; font-size: 14px; font-weight: bold; border-radius: 8px; display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px;}
.active, .collapsible:hover {background-color: #f9f9f9; border-color:#409EFF}
.collapsible:after {content: '▼';} .active:after {content: '▲';}
.content {padding: 0 15px; display: none; overflow: hidden; background-color: white; border: 1px solid #eee; border-top:none; border-radius: 0 0 8px 8px; margin-top:-10px; margin-bottom: 20px;}
table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:12px 8px;border-bottom:1px solid #f0f0f0;font-size:12px}
.page-btn{padding:5px 12px; background:#fff; border:1px solid #ddd; cursor:pointer; border-radius:4px}
.page-btn:disabled{color:#ccc; cursor:not-allowed}
</style></head><body>

<div class="card"><div style="display:flex;justify-content:space-between;align-items:center">
<div><h2>🚀 智能清理 (同步指纹版)</h2>
<span class="tag" style="background:#67C23A" id="statusWatch">监控: --</span>
<span class="tag" style="background:#409EFF">索引: <b id="cacheCount">0</b></span>
</div><button class="btn" style="background:#E6A23C" onclick="doClean()">🧹 立即全量扫描</button></div></div>

<form action="/api/save_cfg" method="POST"><div class="grid"><div class="card"><h4>🛠️ 清理规则</h4>
广告后缀: <input type="text" name="ad_exts" value="{{conf.cfg.ad_exts}}">
视频后缀: <input type="text" name="video_exts" value="{{conf.cfg.video_exts}}">
体积阈值(MB): <input type="number" name="size_limit" value="{{conf.cfg.size_limit}}" step="0.1">
排除目录: <input type="text" name="exclude_dirs" value="{{conf.cfg.exclude_dirs}}"></div>
<div class="card"><h4>系统设置</h4>
实时监控: <select id="watchSelect" name="watch_enabled"><option value="on">开启</option><option value="off">关闭</option></select>
<button type="submit" class="btn" style="width:100%;margin-top:20px">💾 保存规则</button></div></div></form>

<div class="card"><h4>📍 监控目录</h4>
<div class="grid" style="grid-template-columns: 1fr auto auto; gap:10px">
<input type="text" id="p" name="path" readonly placeholder="点击选择..." style="margin:0">
<button type="button" class="btn" onclick="showP()" style="background:#909399">📁 浏览</button>
<button type="button" class="btn" onclick="addTask()" style="background:green">➕ 添加</button></div>
<div id="taskList" style="margin-top:15px"></div></div>

<button type="button" class="collapsible">⚡ 扫描缓存详情 (实时同步)</button>
<div class="content"><div style="padding: 15px 0;">
    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:10px">
        <input type="text" id="cacheSearch" placeholder="搜索缓存目录..." style="width:200px; margin:0" oninput="resetPage()">
        <div style="display:flex; gap:10px">
            <button class="btn" style="background:#F56C6C; padding:5px 12px; font-size:12px" onclick="clearCache()">🗑️ 清空缓存</button>
            <div id="pagination"></div>
        </div>
    </div>
    <table id="cacheTable"><thead><tr><th>扫描路径</th><th>文件数</th><th>快照时间</th></tr></thead><tbody id="cacheBody"></tbody></table>
</div></div>

<button type="button" class="collapsible">📜 运行日志</button>
<div class="content"><div style="padding: 15px 0;">
    <div style="display:flex; justify-content:flex-end; margin-bottom:10px">
        <button class="btn" style="background:#F56C6C; padding:5px 12px; font-size:12px" onclick="clearLog()">🗑️ 清空日志</button>
    </div>
    <div id="logBox" class="log-area">同步中...</div>
</div></div>

<div id="picker"><h4 id="ch">/CloudNAS</h4><div id="ls" style="height:280px;overflow:auto;border:1px solid #eee;margin-bottom:15px"></div>
<div style="display:flex; justify-content:flex-end; gap:10px"><button class="btn" style="background:#909399" onclick="hideP()">取消</button><button class="btn" onclick="confirmP()">选择</button></div></div>

<script>
let curPage = 1, pageSize = 15, cachedData = { cache: {} };
function resetPage() { curPage = 1; renderCache(); }
function renderCache() {
    const search = document.getElementById('cacheSearch').value.toLowerCase();
    const allKeys = Object.keys(cachedData.cache).filter(k => k.toLowerCase().includes(search));
    const totalPages = Math.ceil(allKeys.length / pageSize) || 1;
    if(curPage > totalPages) curPage = totalPages;
    const pageKeys = allKeys.slice((curPage-1)*pageSize, curPage*pageSize);
    let html = '';
    pageKeys.forEach(path => {
        let val = cachedData.cache[path] || "0_0";
        let [count, mtime] = val.split('_');
        html += `<tr><td>${path.replace('/CloudNAS/','')}</td><td>${count}</td><td>${new Date(mtime*1000).toLocaleString()}</td></tr>`;
    });
    document.getElementById('cacheBody').innerHTML = html || '<tr><td colspan="3" style="text-align:center">无记录</td></tr>';
    document.getElementById('pagination').innerHTML = `<button class="page-btn" onclick="curPage--;renderCache()" ${curPage==1?'disabled':''}>&lt;</button> ${curPage}/${totalPages} <button class="page-btn" onclick="curPage++;renderCache()" ${curPage>=totalPages?'disabled':''}>&gt;</button>`;
}

function refreshData() {
    fetch('/api/get_all_data').then(r => r.json()).then(data => {
        cachedData = data;
        document.getElementById('statusWatch').innerText = "监控: " + (data.conf.cfg.watch_enabled == 'on' ? '开启' : '关闭');
        let tHtml = '';
        data.conf.tasks.forEach(t => {
            tHtml += `<span class="tag" style="background:#f0f2f5; color:#333; border:1px solid #ddd; display:inline-block; margin-bottom:5px">📁 ${t.path} <a href="/del_task/${t.id}" style="color:red; margin-left:8px; text-decoration:none">×</a></span>`;
        });
        document.getElementById('taskList').innerHTML = tHtml;
        document.getElementById('logBox').innerHTML = data.logs.join('');
        document.getElementById('cacheCount').innerText = Object.keys(data.cache).length;
        renderCache();
    });
}
function addTask(){ 
    const p = document.getElementById('p').value;
    const f = new FormData(); f.append('path', p);
    fetch('/api/add_task', {method:'POST', body:f}).then(()=>refreshData());
}
function doClean(){fetch('/api/manual_clean'); alert("全量扫描已开始");}
function clearCache(){if(confirm("确定要清空缓存吗？")){fetch('/api/clear_cache').then(()=>refreshData());}}
function clearLog(){if(confirm("确定要清空日志吗？")){fetch('/api/clear_log').then(()=>refreshData());}}
let curPath=""; function showP(){document.getElementById('picker').style.display='block';loadDir("");}
function hideP(){document.getElementById('picker').style.display='none';}
function loadDir(p){curPath=p;document.getElementById('ch').innerText="/CloudNAS/"+p;fetch('/api/list_dir?path='+encodeURIComponent(p)).then(r=>r.json()).then(data=>{
    let h=p?`<div onclick="loadDir('${p.split('/').slice(0,-1).join('/')}')" style="color:#409EFF;cursor:pointer;padding:8px">🔙 上级</div>`:"";
    if(data.success) data.dirs.forEach(d=>h+=`<div style="padding:8px;cursor:pointer;border-bottom:1px solid #eee" onclick="loadDir('${p?p+'/'+d:d}')">📁 ${d}</div>`);
    document.getElementById('ls').innerHTML=h;
});}
function confirmP(){document.getElementById('p').value=curPath;hideP();}
setInterval(refreshData, 5000); refreshData();
var coll = document.getElementsByClassName("collapsible");
for (var i = 0; i < coll.length; i++) {
  coll[i].addEventListener("click", function() {
    this.classList.toggle("active");
    var content = this.nextElementSibling;
    content.style.display = (content.style.display === "block") ? "none" : "block";
  });
}
</script></body></html>
"""

if __name__ == '__main__':
    if not os.path.exists(CONFIG_PATH): save_json(CONFIG_PATH, DEFAULT_CONFIG)
    if not os.path.exists(CACHE_PATH): save_json(CACHE_PATH, {})
    threading.Thread(target=watchdog_worker, daemon=True).start()
    app.run(host='0.0.0.0', port=5000)