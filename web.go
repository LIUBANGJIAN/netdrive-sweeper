package main

import "html/template"

var pageTpl = template.Must(template.New("page").Parse(pageHTML))

const pageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>
:root{--bg:#0f172a;--muted:#94a3b8;--text:#e5e7eb;--line:#243044;--ok:#22c55e;--bad:#ef4444}
*{box-sizing:border-box}body{margin:0;background:linear-gradient(135deg,#0f172a,#111827);font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:var(--text)}
header{padding:28px 32px;border-bottom:1px solid var(--line);background:rgba(15,23,42,.86);position:sticky;top:0;backdrop-filter:blur(12px);z-index:1}
h1{margin:0;font-size:26px}.sub,.muted{color:var(--muted)}.wrap{padding:24px;max-width:1280px;margin:auto}.grid{display:grid;grid-template-columns:1fr 1fr;gap:18px}
.card{background:rgba(17,24,39,.92);border:1px solid var(--line);border-radius:18px;padding:20px;box-shadow:0 20px 45px rgba(0,0,0,.25)}
label{display:block;margin:12px 0 6px;color:#cbd5e1;font-size:13px}input,textarea{width:100%;border:1px solid #334155;background:#0b1220;color:var(--text);border-radius:12px;padding:11px 12px;outline:none}textarea{min-height:92px}
.row{display:flex;gap:10px;flex-wrap:wrap}.btn{border:0;border-radius:12px;padding:11px 15px;background:#1f2937;color:var(--text);cursor:pointer}.btn.primary{background:linear-gradient(135deg,#0284c7,#06b6d4)}.btn.danger{background:#991b1b}.btn.ok{background:#166534}
.pill{display:inline-flex;padding:4px 9px;border-radius:999px;background:#172554;color:#bfdbfe;margin:3px;font-size:12px}.oktxt{color:var(--ok)}.badtxt{color:var(--bad)}
table{width:100%;border-collapse:collapse}td,th{border-bottom:1px solid var(--line);padding:10px;text-align:left;font-size:13px}pre{white-space:pre-wrap;max-height:260px;overflow:auto;background:#020617;padding:14px;border-radius:12px}
@media(max-width:900px){.grid{grid-template-columns:1fr}}
</style>
</head>
<body>
<header><h1>NetDrive Sweeper Go</h1><div class="sub">CloudDrive2 官方 gRPC Token API · 目录范围完全由 TokenInfo.rootDir 和权限决定</div></header>
<div class="wrap">
<div class="grid">
<section class="card"><h2>连接与规则</h2>
<label>CD2 gRPC 地址</label><input id="address" placeholder="192.168.10.252:19798">
<label>API Token</label><input id="token" type="password">
<div class="grid"><div><label>广告后缀</label><input id="adExts"></div><div><label>视频后缀</label><input id="videoExts"></div><div><label>小视频阈值 MB</label><input id="sizeLimit" type="number" step="0.1"></div><div><label>最大深度，0 不限制</label><input id="maxDepth" type="number"></div></div>
<label>排除目录关键词</label><input id="excludeDirs">
<label>清理目录，建议通过右侧目录浏览按钮添加；支持多个目录，一行一个；/ 表示 Token 授权根</label><textarea id="tasks"></textarea>
<div class="row"><label><input id="forceRefresh" type="checkbox" style="width:auto"> 强制刷新 CD2 缓存</label><label><input id="offlineOnly" type="checkbox" style="width:auto"> 只清理已完成离线任务目录</label><label><input id="deletePermanently" type="checkbox" style="width:auto"> 永久删除</label></div>
<label>扫描延迟 ms</label><input id="scanDelay" type="number">
<div class="row" style="margin-top:16px"><button class="btn primary" id="saveBtn">保存配置</button><button class="btn" id="testBtn">测试 Token</button><button class="btn ok" id="scanBtn">扫描预览</button><button class="btn danger" id="cleanBtn">执行清理</button></div>
</section>
<section class="card"><h2>Token 状态</h2><div id="status" class="muted">加载中...</div><div id="perms" style="margin-top:12px"></div><h3>目录浏览</h3><div class="row"><input id="browsePath" value="/" style="flex:1"><button class="btn" id="listBtn">加载</button></div><div id="dirs"></div></section>
</div>
<section class="card" style="margin-top:18px"><h2>结果</h2><div id="result" class="muted">暂无</div></section>
<section class="card" style="margin-top:18px"><div class="row"><h2 style="flex:1">日志</h2><button class="btn" id="logsBtn">刷新日志</button><button class="btn danger" id="clearLogsBtn">清空日志</button></div><pre id="logs"></pre></section>
</div>
<script>
let state={};
async function api(url,opt){let r=await fetch(url,opt);let j=await r.json();if(!r.ok||j.ok===false)throw new Error(j.error||'请求失败');return j}
function val(id){return document.getElementById(id).value}function setv(id,v){document.getElementById(id).value=v??''}function checked(id){return document.getElementById(id).checked}
function esc(s){return String(s??'').replace(/[&<>"']/g,function(m){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m]})}
async function load(){let j=await api('/api/state');state=j;let c=j.config;setv('address',c.address);setv('token',c.token);setv('adExts',c.ad_exts);setv('videoExts',c.video_exts);setv('sizeLimit',c.size_limit_mb);setv('excludeDirs',c.exclude_dirs);setv('scanDelay',c.scan_delay_ms);setv('maxDepth',c.max_depth);setv('tasks',(c.tasks||['/']).join('\n'));document.getElementById('forceRefresh').checked=!!c.force_refresh;document.getElementById('offlineOnly').checked=c.offline_only!==false;document.getElementById('deletePermanently').checked=!!c.delete_permanently;renderStatus(j.status);loadLogs()}
function renderStatus(s){document.getElementById('status').innerHTML=esc(s.last_message||'');let t=s.token;if(!t){document.getElementById('perms').innerHTML='<span class="muted">尚未读取 TokenInfo</span>';return}let h='<div>根目录：<b>'+esc(t.rootDir)+'</b></div><div>名称：'+esc(t.friendlyName||'-')+'</div><div>';h+='<span class="pill">allow_list '+(t.allowList?'OK':'NO')+'</span>';h+='<span class="pill">allow_delete '+(t.allowDelete?'OK':'NO')+'</span>';h+='<span class="pill">allow_delete_permanently '+(t.allowDeletePermanently?'OK':'NO')+'</span></div>';document.getElementById('perms').innerHTML=h}
async function saveCfg(){let c={address:val('address'),token:val('token'),ad_exts:val('adExts'),video_exts:val('videoExts'),size_limit_mb:parseFloat(val('sizeLimit')||0),exclude_dirs:val('excludeDirs'),scan_delay_ms:parseInt(val('scanDelay')||0),max_depth:parseInt(val('maxDepth')||0),force_refresh:checked('forceRefresh'),offline_only:checked('offlineOnly'),delete_permanently:checked('deletePermanently'),tasks:taskList()};await api('/api/save',{method:'POST',body:JSON.stringify(c)});document.getElementById('result').innerHTML='<span class="oktxt">已保存</span>';await load()}
async function testConn(){try{await saveCfg();let j=await api('/api/test');renderStatus({last_message:j.message,token:j.token});document.getElementById('result').innerHTML='<span class="oktxt">'+esc(j.message)+'</span>'}catch(e){document.getElementById('result').innerHTML='<span class="badtxt">'+esc(e.message)+'</span>'}}
async function listDir(){try{let j=await api('/api/list?path='+encodeURIComponent(val('browsePath')||'/'));renderStatus({last_message:'目录加载成功 '+j.displayPath,token:j.token});let h='<table><tr><th>目录</th><th>操作</th></tr>';if(val('browsePath')!='/'){let up=val('browsePath').split('/').slice(0,-1).join('/')||'/';h+='<tr><td>..</td><td><button class="btn enter" data-path="'+esc(up)+'">返回上级</button></td></tr>'}for(let d of j.dirs){h+='<tr><td>'+esc(d.displayPath)+'</td><td><button class="btn enter" data-path="'+esc(d.path)+'">进入</button><button class="btn add" data-path="'+esc(d.path)+'">加入清理</button></td></tr>'}document.getElementById('dirs').innerHTML=h+'</table>';document.querySelectorAll('.enter').forEach(b=>b.onclick=function(){setv('browsePath',this.dataset.path);listDir()});document.querySelectorAll('.add').forEach(b=>b.onclick=function(){addTask(this.dataset.path)})}catch(e){document.getElementById('dirs').innerHTML='<p class="badtxt">'+esc(e.message)+'</p>'}}
function taskList(){return val('tasks').split(/\n+/).map(x=>x.trim()).filter(Boolean)}
function addTask(p){let t=taskList();if(!t.includes(p))t.push(p);setv('tasks',t.join('\n'))}
function clearTasks(){setv('tasks','')}
async function scan(del){try{await saveCfg();let j=await api(del?'/api/clean':'/api/scan');let h='<p>检查 '+j.checked+'，命中 '+j.matched+'，删除 '+j.deleted+'</p>';if(j.offlineTasks&&j.offlineTasks.length){h+='<h3>离线任务目录状态</h3><table><tr><th>目录</th><th>状态</th><th>说明</th></tr>';for(let o of j.offlineTasks){h+='<tr><td>'+esc(o.displayPath)+'</td><td>'+(o.ready?'<span class="oktxt">':'<span class="badtxt">')+esc(o.status)+'</span></td><td>'+esc(o.note)+'</td></tr>'}h+='</table>'}if(j.errors&&j.errors.length)h+='<p class="badtxt">'+j.errors.map(esc).join('<br>')+'</p>';h+='<h3>命中的垃圾文件</h3><table><tr><th>文件</th><th>大小</th></tr>';for(let x of (j.items||[])){h+='<tr><td>'+esc(x.displayPath)+'</td><td>'+x.size+'</td></tr>'}document.getElementById('result').innerHTML=h+'</table>';loadLogs()}catch(e){document.getElementById('result').innerHTML='<span class="badtxt">'+esc(e.message)+'</span>'}}
async function loadLogs(){let j=await api('/api/logs');document.getElementById('logs').textContent=j.logs||''}async function clearLogs(){await api('/api/clear_logs');loadLogs()}
document.getElementById('saveBtn').onclick=saveCfg;document.getElementById('testBtn').onclick=testConn;document.getElementById('scanBtn').onclick=function(){scan(false)};document.getElementById('cleanBtn').onclick=function(){scan(true)};document.getElementById('listBtn').onclick=listDir;document.getElementById('addCurrentBtn').onclick=function(){addTask(val('browsePath')||'/')};document.getElementById('clearTasksBtn').onclick=clearTasks;document.getElementById('logsBtn').onclick=loadLogs;document.getElementById('clearLogsBtn').onclick=clearLogs;
load().catch(e=>document.body.insertAdjacentHTML('beforeend','<pre>'+esc(e.message)+'</pre>'))
</script>
</body></html>`
