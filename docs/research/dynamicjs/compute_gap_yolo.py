import cv2, numpy as np, sys
from captcha_recognizer.slider import Slider
bg=cv2.imread(sys.argv[1],cv2.IMREAD_UNCHANGED)
pz=cv2.imread(sys.argv[2],cv2.IMREAD_UNCHANGED)
bgr=bg[:,:,:3] if bg.ndim==3 and bg.shape[2]==4 else bg
r=Slider().identify(bgr[:,:,::-1])
box=r[0]; conf=float(r[1]); box_left=float(box[0])
a=pz[:,:,3] if pz.ndim==3 and pz.shape[2]==4 else cv2.cvtColor(pz,cv2.COLOR_BGR2GRAY)
ys,xs=np.where(a>128); ex0=int(xs.min()) if len(xs) else 0
gap=box_left-ex0
print(f'YOLO conf={conf:.3f} box_left={box_left:.1f} ex0={ex0} gap={int(round(gap))}')
