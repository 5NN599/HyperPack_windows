import os, sys, subprocess, threading, queue, tkinter as tk
from tkinter import filedialog, messagebox, ttk
from pathlib import Path

BG="#0b1020"; CARD="#121a2b"; CARD2="#182238"; TEXT="#eef4ff"; MUTED="#90a3bf"; ACC="#63e6be"; ACC2="#5aa9ff"; ERR="#ff6b6b"

class App(tk.Tk):
    def __init__(self):
        super().__init__(); self.title("HyperPack 0.3.0"); self.geometry("860x610"); self.configure(bg=BG); self.minsize(760,540)
        self.tasks=queue.Queue(); self.current=[]
        self.core=self.find_core(); self.level=tk.IntVar(value=9); self.threads=tk.IntVar(value=max(1,(os.cpu_count() or 4))); self.status=tk.StringVar(value="대기 중")
        self.build_ui(); self.after(120,self.poll)
    def find_core(self):
        base=Path(sys.argv[0]).resolve().parent
        candidates=[]
        meipass=getattr(sys,"_MEIPASS",None)
        if meipass: candidates += [Path(meipass)/"core"/"hyperpack.exe", Path(meipass)/"core"/"hyperpack"]
        candidates += [base/"hyperpack", base/"hyperpack.exe", base/"bin"/"hyperpack.exe", base/"dist"/"linux"/"hyperpack"]
        for p in candidates:
            if p.exists(): return str(p)
        return "hyperpack.exe"
    def label(self,parent,text,size=11,weight="normal",fg=TEXT): return tk.Label(parent,text=text,bg=parent["bg"],fg=fg,font=("Segoe UI",size,weight))
    def button(self,parent,text,cmd,bg):
        return tk.Button(parent,text=text,command=cmd,bg=bg,fg=TEXT,activebackground="#2b4168",activeforeground=TEXT,relief="flat",bd=0,font=("Segoe UI",12,"bold"),height=2,cursor="hand2")
    def build_ui(self):
        root=tk.Frame(self,bg=BG); root.pack(fill="both",expand=True,padx=26,pady=24)
        head=tk.Frame(root,bg=BG); head.pack(fill="x")
        self.label(head,"HYPERPACK",26,"bold",ACC).pack(anchor="w"); self.label(head,"0.3.0  •  Solid Stream  •  Huffman + Large Dictionary",11,"normal",MUTED).pack(anchor="w",pady=(2,18))
        cards=tk.Frame(root,bg=BG); cards.pack(fill="x")
        self.button(cards,"파일 묶어서 압축",self.pack_files,ACC2).grid(row=0,column=0,sticky="ew",padx=(0,8)); self.button(cards,"폴더 전체 압축",self.pack_folder,"#7c5cff").grid(row=0,column=1,sticky="ew",padx=8); self.button(cards,"압축 해제",self.extract,"#3cba92").grid(row=0,column=2,sticky="ew",padx=(8,0)); cards.grid_columnconfigure((0,1,2),weight=1)
        panel=tk.Frame(root,bg=CARD); panel.pack(fill="x",pady=18)
        self.label(panel,"압축 설정",12,"bold").grid(row=0,column=0,padx=18,pady=(14,6),sticky="w")
        tk.Label(panel,text="Level",bg=CARD,fg=MUTED,font=("Segoe UI",10)).grid(row=1,column=0,padx=18,pady=(0,14),sticky="w")
        ttk.Combobox(panel,values=list(range(10)),textvariable=self.level,state="readonly",width=8).grid(row=1,column=1,padx=4,pady=(0,14),sticky="w")
        tk.Label(panel,text="Threads",bg=CARD,fg=MUTED,font=("Segoe UI",10)).grid(row=1,column=2,padx=18,pady=(0,14),sticky="w")
        ttk.Spinbox(panel,from_=1,to=max(1,(os.cpu_count() or 4)),textvariable=self.threads,width=8).grid(row=1,column=3,padx=4,pady=(0,14),sticky="w")
        self.label(panel,"Level 9: 96 MiB dictionary + solid groups + entropy-coded tokens",10,"normal",MUTED).grid(row=2,column=0,columnspan=4,padx=18,pady=(0,14),sticky="w")
        status=tk.Frame(root,bg=CARD2); status.pack(fill="both",expand=True)
        self.label(status,"PROCESS",10,"bold",MUTED).pack(anchor="w",padx=18,pady=(16,4)); self.label(status,"",11).pack()
        self.progress=ttk.Progressbar(status,mode="determinate",maximum=100); self.progress.pack(fill="x",padx=18,pady=14)
        self.label(status,"",10).pack()
        tk.Label(status,textvariable=self.status,bg=CARD2,fg=TEXT,font=("Segoe UI",12,"bold"),wraplength=780,justify="left").pack(anchor="w",padx=18)
        self.label(status,"",10).pack(); self.label(status,"0.3.0은 HPK3 실험 포맷입니다. 목적은 장거리 중복 + 솔리드 스트림 + 엔트로피 코딩입니다.",10,"normal",MUTED).pack(anchor="w",padx=18,pady=(0,18))
    def run(self,args):
        self.status.set("작업 시작..."); self.progress["value"]=0
        def worker():
            try:
                p=subprocess.Popen(args,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,bufsize=1,creationflags=(0x08000000 if os.name=="nt" else 0))
                for line in p.stdout: self.tasks.put(line.rstrip())
                code=p.wait(); self.tasks.put(("DONE",code))
            except Exception as e: self.tasks.put(("ERROR",str(e)))
        threading.Thread(target=worker,daemon=True).start()
    def core_cmd(self,sub,args): return [self.core,sub,*args]
    def pack_files(self):
        paths=filedialog.askopenfilenames(title="압축할 파일 선택");
        if not paths:return
        out=filedialog.asksaveasfilename(defaultextension=".hpk",filetypes=[("HyperPack 3","*.hpk"),("All files","*.*")]);
        if not out:return
        args=[out,*paths,"--level",str(self.level.get()),"--threads",str(self.threads.get())]; self.run(self.core_cmd("pack-file",args))
    def pack_folder(self):
        folder=filedialog.askdirectory(title="압축할 폴더 선택");
        if not folder:return
        out=filedialog.asksaveasfilename(defaultextension=".hpk",filetypes=[("HyperPack 3","*.hpk"),("All files","*.*")]);
        if not out:return
        self.run(self.core_cmd("pack-folder",[out,folder,"--level",str(self.level.get()),"--threads",str(self.threads.get())]))
    def extract(self):
        src=filedialog.askopenfilename(filetypes=[("HyperPack","*.hpk"),("All files","*.*")]);
        if not src:return
        out=filedialog.askdirectory(title="복원 폴더 선택");
        if not out:return
        self.run(self.core_cmd("extract",[src,out]))
    def poll(self):
        try:
            while True:
                x=self.tasks.get_nowait()
                if isinstance(x,tuple):
                    if x[0]=="DONE": self.status.set("완료" if x[1]==0 else f"실패 (코드 {x[1]})")
                    else: self.status.set("실행 오류: "+x[1])
                    continue
                self.status.set(x)
                import re
                m=re.search(r'(\d+)%',x)
                if m:self.progress["value"]=int(m.group(1))
        except queue.Empty: pass
        self.after(120,self.poll)

if __name__=="__main__": App().mainloop()
