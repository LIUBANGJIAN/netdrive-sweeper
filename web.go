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
:root{--bg:#0d1117;--panel:#161b22;--card:#21262d;--line:#30363d;--muted:#8b949e;--text:#e6edf3;--blue:#58a6ff;--green:#3fb950;--red:#f85149;--orange:#d29922;--purple:#a371f7}
*{box-sizing:border-box}body{margin:0;background:var(--bg);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:var(--text);font-size:14px;min-height:100vh}
.header{display:flex;align-items:center;justify-content:space-between;padding:16px 20px;border-bottom:1px solid var(--line);background:var(--panel);position:sticky;top:0;z-index:10}
.logo{display:flex;align-items:center;gap:10px}.logo h1{margin:0;font-size:17px;font-weight:700;color:var(--blue)}.logo small{color:var(--muted);font-size:12px}
.status{display:flex;align-items:center;gap:12px}.status .dot{width:8px;height:8px;border-radius:50%;background:var(--green);box-shadow:0 0 8px rgba(63,185,80,.4)}.status .text{color:var(--muted);font-size:12px}
.main{padding:18px;max-width:1400px;margin:auto}
.grid{display:grid;grid-template-columns:1fr 380px;gap:16px}
.left{display:grid;gap:16px}
.right{display:grid;gap:16px}
.card{background:var(--panel);border:1px solid var(--line);border-radius:16px;padding:18px}.card h2{margin:0 0 14px;font-size:15px;font-weight:600;color:#c9d1d9}
.formgroup{margin-bottom:12px}
.formgroup label{display:block;margin:0 0 5px;color:var(--muted);font-size:12px;font-weight:500}
.formgroup input,.formgroup textarea{width:100%;border:1px solid var(--line);background:var(--bg);color:var(--text);border-radius:8px;padding:9px 11px;outline:none;font-size:13px;transition:.15s}
.formgroup input:focus,.formgroup textarea:focus{border-color:var(--blue);box-shadow:0 0 0 3px rgba(88,166,255,.1)}
.formgroup textarea{min-height:80px;resize:vertical}
.row2{display:grid;grid-template-columns:1fr 1fr;gap:10px}
.row3{display:grid;grid-template-columns:1fr 1fr 1fr;gap:10px}
.checks{display:flex;gap:10px;flex-wrap:wrap;margin-top:12px}
.check{display:flex;align-items:center;gap:6px;border:1px solid var(--line);border-radius:6px;padding:6px 9px;background:var(--bg);font-size:12px;color:var(--text)}.check input{width:auto}
.btn{border:0;border-radius:8px;padding:9px 12px;font-size:13px;font-weight:600;cursor:pointer;transition:.15s}
.btn-primary{background:#238636;color:white}.btn-primary:hover{background:#2ea043}
.btn-ok{background:#0969da;color:white}.btn-ok:hover{background:#0d6eaf}
.btn-danger{background:#da3633;color:white}.btn-danger:hover{background:#f85149}
.btn-ghost{background:var(--bg);border:1px solid var(--line);color:var(--text)}.btn-ghost:hover{background:var(--card)}
.actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:14px}
.statline{display:flex;justify-content:space-between;padding:10px 12px;background:var(--bg);border-radius:8px;margin-bottom:8px}.statline .label{color:var(--muted);font-size:12px}.statline .value{font-weight:600}
.dirpanel{max-height:280px;overflow:auto;border:1px solid var(--line);border-radius:8px;background:var(--bg)}.dirpanel::-webkit-scrollbar{width:6px}.dirpanel::-webkit-scrollbar-track{background:var(--bg)}.dirpanel::-webkit-scrollbar-thumb{background:var(--line);border-radius:999px}
.diritem{display:flex;align-items:center;gap:8px;padding:8px 10px;border-bottom:1px solid #1c2128;cursor:pointer;transition:.1s}.diritem:hover{background:var(--card)}.diritem:last-child{border-bottom:0}.diritem .chk{width:14px;height:14px}.diritem .name{flex:1;font-size:13px}.diritem .add{color:var(--blue);font-size:12px;opacity:.7}.diritem:hover .add{opacity:1}
.tasklist{max-height:120px;overflow:auto;border:1px solid var(--line);border-radius:8px;background:var(--bg);padding:6px}.taskitem{display:flex;align-items:center;justify-content:space-between;padding:6px 8px;border-radius:6px;margin-bottom:3px;background:rgba(255,255,255,.02);font-size:12px;color:#c9d1d9}.taskitem:hover{background:rgba(255,255,255,.06)}.taskitem .del{color:var(--muted);cursor:pointer;font-size:14px}.taskitem .del:hover{color:var(--red)}
.pathbar{display:flex;gap:6px;margin-bottom:10px}.pathbar input{flex:1}.pathbar button{white-space:nowrap}
.resultbox{min-height:100px;padding:12px;background:var(--bg);border-radius:8px;border:1px solid var(--line);font-size:13px;color:var(--muted)}
.logbox{max-height:240px;overflow:auto;padding:12px;background:#010409;border-radius:8px;border:1px solid var(--line);font-size:12px;color:#8b949e;white-space:pre-wrap;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace}
.toast{position:fixed;right:18px;bottom:18px;background:#0e4429;color:#6ee7b7;border:1px solid #26a641;border-radius:10px;padding:11px 14px;font-size:13px;box-shadow:0 8px 24px rgba(0,0,0,.4);display:none;z-index:100}.toast.bad{background:#490202;color:#ff7b72;border-color:#da3633}
@media(max-width:1000px){.grid{grid-template-columns:1fr}.row2{grid-template-columns:1fr}.row3{grid-template-columns:1fr}.pathbar{flex-wrap:wrap}}
</style>
</head>
<body>
<div class="header">
  <div class="logo"><h1>NetDrive Sweeper</h1><small>CloudDrive2 gRPC 清理工具</small></div>
  <div class="status"><span class="dot" id="statusDot"></span><span class="text" id="statusText">未连接</span></div>
</div>
<main class="main">
<div class="grid">
<div class="left">
<div class="card">
  <h2>CD2 连接</h2>
  <div class="formgroup"><label>gRPC 地址</label><input id="address" placeholder="192.168.10.252:19798"></div>
  <div class="formgroup"><label>API Token</label><input id="token" type="password" autocomplete="off"></div>
  <div class="actions"><button class="btn btn-ok" id="testBtn">测试连接</button><button class="btn btn-primary" id="saveBtn">保存配置</button></div>
  <div style="margin-top:12px;padding:10px;background:var(--bg);border-radius:8px;border:1px solid var(--line);font-size:12px;color:var(--muted)">
    Token 根目录: <span id="tokenRoot" style="color:var(--text)">-</span>
    <div style="margin-top:6px">权限: <span id="tokenPerms"></span></div>
  </div>
</div>
<div class="card">
  <h2>清理规则</h2>
  <div class="row2">
    <div class="formgroup"><label>广告后缀</label><input id="adExts" value=".txt,.html,.url,.lnk"></div>
    <div class="formgroup"><label>视频后缀</label><input id="videoExts" value=".mp4,.mkv,.ts"></div>
    <div class="formgroup"><label>小视频阈值 MB</label><input id="sizeLimit" type="number" step="0.1" value="5"></div>
    <div class="formgroup"><label>最大深度</label><input id="maxDepth" type="number" value="0"></div>
    <div class="formgroup"><label>排除关键词</label><input id="excludeDirs" value="重要,备份"></div>
    <div class="formgroup"><label>扫描延迟 ms</label><input id="scanDelay" type="number" value="50"></div>
  </div>
  <div class="checks">
    <label class="check"><input id="forceRefresh" type="checkbox">强制刷新 CD2 缓存</label>
    <label class="check"><input id="offlineOnly" type="checkbox" checked>只清理已完成离线任务</label>
    <label class="check"><input id="deletePermanently" type="checkbox">永久删除</label>
  </div>
</div>
<div class="card">
  <h2>清理目录</h2>
  <div class="tasklist" id="taskList"></div>
  <div class="actions" style="margin-top:10px"><button class="btn btn-ghost" id="clearTasksBtn">清空目录</button></div>
</div>
<div class="card">
  <h2>扫描与清理</h2>
  <div class="statline"><span class="label">检查文件</span><span class="value" id="statChecked">0</span></div>
  <div class="statline"><span class="label">命中垃圾</span><span class="value" id="statMatched">0</span></div>
  <div class="statline"><span class="label">已删除</span><span class="value" id="statDeleted">0</span></div>
  <div class="statline"><span class="label">错误</span><span class="value" id="statErrors">0</span></div>
  <div class="actions"><button class="btn btn-primary" id="scanBtn">扫描预览</button><button class="btn btn-danger" id="cleanBtn">执行清理</button></div>
</div>
</div>
<div class="right">
<div class="card">
  <h2>目录浏览</h2>
  <div class="pathbar">
    <input id="browsePath" value="/">
    <button class="btn btn-ghost" id="listBtn">加载</button>
    <button class="btn btn-ok" id="addCurrentBtn">加入</button>
    <button class="btn btn-primary" id="addAllBtn">加入全部</button>
  </div>
  <div style="font-size:12px;color:var(--muted);margin-bottom:8px">浏览到离线任务完成的目录后加入清理</div>
  <div class="dirpanel" id="dirPanel"></div>
</div>
<div class="card">
  <h2>扫描结果</h2>
  <div class="resultbox" id="resultBox">点击"扫描预览"查看结果</div>
</div>
<div class="card">
  <h2>运行日志</h2>
  <div class="logbox" id="logsBox"></div>
  <div class="actions" style="margin-top:10px"><button class="btn btn-ghost" id="logsBtn">刷新</button><button class="btn btn-danger" id="clearLogsBtn">清空</button></div>
</div>
</div>
</div>
</main>
<div id="toast" class="toast"></div>
<input id="tasksHidden" type="hidden">
<script>
let state={};
async function api(url,opt){let r=await fetch(url,{cache:'no-store',headers:{'Content-Type':'application/json'},...(opt||{})});let j=await r.json();if(!r.ok||j.ok===false)throw new Error(j.error||'请求失败');return j}
function el(id){return document.getElementById(id)}function val(id){return el(id).value}function setv(id,v){el(id).value=v??''}function checked(id){return el(id).checked}function esc(s){return String(s??'').replace(/[&<>"']/g,m=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m]))}
function toast(msg,bad){let t=el('toast');t.textContent=msg;t.className='toast'+(bad?' bad':'');t.style.display='block';setTimeout(()=>t.style.display='none',2800)}
function taskList(){return val('tasksHidden').split('\n').map(x=>x.trim()).filter(Boolean)}
function setTaskList(list){setv('tasksHidden',list.join('\n'));renderTaskList(list)}
function renderTaskList(list){if(!list||!list.length){el('taskList').innerHTML='<div style="padding:10px;text-align:center;color:var(--muted)">尚未添加目录</div>';return}el('taskList').innerHTML=list.map(p=>'<div class="taskitem"><span>'+esc(p)+'</span><span class="del" onclick="removeTask('+JSON.stringify(p)+')">×</span></div>').join('')}
function removeTask(p){let t=taskList();t=t.filter(x=>x!==p);setTaskList(t)}
function fillConfig(c){setv('address',c.address);setv('token',c.token);setv('adExts',c.ad_exts);setv('videoExts',c.video_exts);setv('sizeLimit',c.size_limit_mb);setv('excludeDirs',c.exclude_dirs);setv('scanDelay',c.scan_delay_ms);setv('maxDepth',c.max_depth);setTaskList(c.tasks&&c.tasks.length?c.tasks:['/']);el('forceRefresh').checked=!!c.force_refresh;el('offlineOnly').checked=c.offline_only!==false;el('deletePermanently').checked=!!c.delete_permanently}
async function load(){let j=await api('/api/state?_='+Date.now());state=j;fillConfig(j.config);updateStatus(j.status)}
function updateStatus(s){el('statusText').textContent=s.last_message||'未连接';let t=s.token;if(t){el('statusDot').style.background='var(--green)';el('tokenRoot').textContent=t.rootDir;let p=[['list',t.allowList],['delete',t.allowDelete],['delete_permanently',t.allowDeletePermanently]];el('tokenPerms').innerHTML=p.map(x=>'<span style="padding:2px 6px;border-radius:4px;font-size:11px;background:'+(x[1]?'#0e4429':'#490202')+';color:'+(x[1]?'#6ee7b7':'#ff7b72')+'">'+x[0]+'</span>').join(' ')}else{el('statusDot').style.background='var(--muted)';el('tokenRoot').textContent='-';el('tokenPerms').innerHTML='<span style="color:var(--muted)">未读取</span>'}}
async function saveCfg(){let c={address:val('address'),token:val('token'),ad_exts:val('adExts'),video_exts:val('videoExts'),size_limit_mb:parseFloat(val('sizeLimit')||0),exclude_dirs:val('excludeDirs'),scan_delay_ms:parseInt(val('scanDelay')||0),max_depth:parseInt(val('maxDepth')||0),force_refresh:checked('forceRefresh'),offline_only:checked('offlineOnly'),delete_permanently:checked('deletePermanently'),tasks:taskList()};let j=await api('/api/save',{method:'POST',body:JSON.stringify(c)});if(j.config)fillConfig(j.config);toast('配置已保存')}
async function testConn(){try{await saveCfg();let j=await api('/api/test?_='+Date.now());updateStatus({last_message:j.message,token:j.token});toast(j.message)}catch(e){toast(e.message,true);updateStatus({last_message:'连接失败: '+e.message})}}
async function listDir(){try{let path=val('browsePath')||'/';let j=await api('/api/list?path='+encodeURIComponent(path)+'&_='+Date.now());updateStatus({last_message:'目录: '+j.displayPath,token:j.token});let rows=[];if(path!='/'){let up=path.split('/').slice(0,-1).join('/')||'/';rows.push('<div class="diritem"><span class="name" style="color:var(--blue)">.. 返回上级</span></div>')}for(let d of j.dirs){rows.push('<div class="diritem"><input type="checkbox" class="dirchk" data-path="'+esc(d.path)+'" class="chk"><span class="name">'+esc(d.displayPath)+'</span><span class="add" onclick="addTask('+JSON.stringify(d.path)+')">加入</span></div>')}el('dirPanel').innerHTML=rows.join('');document.querySelectorAll('.diritem').forEach((item,i)=>{if(i===0&&path!='/'){item.onclick=function(){setv('browsePath',path.split('/').slice(0,-1).join('/')||'/');listDir()}}else{item.querySelector('.name')?.addEventListener('click',function(){setv('browsePath',item.querySelector('.dirchk')?.dataset.path||'/');listDir()})}});document.querySelectorAll('.dirchk').forEach(c=>c.onchange=updateAddAll)}catch(e){toast(e.message,true);el('dirPanel').innerHTML='<div style="padding:10px;color:var(--red)">'+esc(e.message)+'</div>'}}
function updateAddAll(){let checked=document.querySelectorAll('.dirchk:checked');el('addAllBtn').textContent=checked.length?'加入选中 ('+checked.length+')':'加入全部'}
function addTask(p){let t=taskList();if(!t.includes(p))t.push(p);setTaskList(t);toast('已添加目录')}
function addAllTasks(){let checked=document.querySelectorAll('.dirchk:checked');if(!checked.length){let inputs=document.querySelectorAll('.dirchk');inputs.forEach(i=>i.checked=true);checked=inputs}checked.forEach(c=>addTask(c.dataset.path));toast('已添加选中目录')}
function clearTasks(){setTaskList([]);toast('已清空目录')}
async function scan(del){try{await saveCfg();let j=await api(del?'/api/clean':'/api/scan');el('statChecked').textContent=j.checked;el('statMatched').textContent=j.matched;el('statDeleted').textContent=j.deleted;el('statErrors').textContent=j.errors?j.errors.length:0;let h='';if(j.offlineTasks&&j.offlineTasks.length){h+='<div style="margin-bottom:10px"><strong style="color:var(--text)">离线任务状态</strong><table style="width:100%;margin-top:6px;font-size:12px"><tr><th style="text-align:left;color:var(--muted)">目录</th><th style="text-align:left;color:var(--muted)">状态</th></tr>';for(let o of j.offlineTasks){h+='<tr><td>'+esc(o.displayPath)+'</td><td>'+(o.ready?'<span style="color:var(--green)">'+esc(o.status)+'</span>':'<span style="color:var(--orange)">'+esc(o.status)+'</span>')+'</td></tr>'}h+='</table></div>'}if(j.items&&j.items.length){h+='<div><strong style="color:var(--text)">命中文件 ('+j.items.length+')</strong><table style="width:100%;margin-top:6px;font-size:12px"><tr><th style="text-align:left;color:var(--muted)">文件</th><th style="text-align:right;color:var(--muted)">大小</th></tr>';for(let x of j.items){h+='<tr><td>'+esc(x.displayPath)+'</td><td style="text-align:right">'+formatSize(x.size)+'</td></tr>'}h+='</table></div>'}else{h+='<div style="color:var(--green)">未发现符合条件的垃圾文件</div>'}if(j.errors&&j.errors.length){h+='<div style="margin-top:10px;color:var(--red)">错误: '+j.errors.join('<br>')+'</div>'}el('resultBox').innerHTML=h||'完成';loadLogs().catch(()=>{})}catch(e){toast(e.message,true);el('resultBox').innerHTML='<span style="color:var(--red)">'+esc(e.message)+'</span>'}}
function formatSize(b){if(b<1024)return b+' B';if(b<1024*1024)return (b/1024).toFixed(1)+' KB';return (b/(1024*1024)).toFixed(1)+' MB'}
async function loadLogs(){let j=await api('/api/logs?_='+Date.now());el('logsBox').textContent=j.logs||''}async function clearLogs(){await api('/api/clear_logs',{method:'POST'});loadLogs();toast('日志已清空')}
el('saveBtn').onclick=()=>saveCfg().catch(e=>toast(e.message,true));el('testBtn').onclick=testConn;el('scanBtn').onclick=()=>scan(false);el('cleanBtn').onclick=()=>scan(true);el('listBtn').onclick=listDir;el('addCurrentBtn').onclick=()=>addTask(val('browsePath')||'/');el('addAllBtn').onclick=addAllTasks;el('clearTasksBtn').onclick=clearTasks;el('logsBtn').onclick=loadLogs;el('clearLogsBtn').onclick=clearLogs;
el('browsePath').addEventListener('keydown',e=>{if(e.key==='Enter')listDir()});
load().catch(e=>toast(e.message,true));
</script>
</body></html>`
