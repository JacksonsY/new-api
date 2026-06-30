/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

/**
 * 离火核 — the live 3D energy core.
 *
 * A noise-displaced icosahedron shaded in the 九紫离火 gradient with a
 * fresnel rim, rendered on a transparent canvas so it floats on the white
 * landing. Vanilla three.js (no react-three-fiber): one mesh, one rAF loop,
 * paused when offscreen, fully disposed on unmount. Degrades to a CSS orb
 * when WebGL is unavailable or reduced motion is requested.
 */
import { useEffect, useRef, useState } from 'react'
import * as THREE from 'three'
import { cn } from '@/lib/utils'

const SNOISE = /* glsl */ `
vec3 mod289(vec3 x){return x - floor(x*(1.0/289.0))*289.0;}
vec4 mod289(vec4 x){return x - floor(x*(1.0/289.0))*289.0;}
vec4 permute(vec4 x){return mod289(((x*34.0)+1.0)*x);}
vec4 taylorInvSqrt(vec4 r){return 1.79284291400159 - 0.85373472095314 * r;}
float snoise(vec3 v){
  const vec2 C = vec2(1.0/6.0, 1.0/3.0);
  const vec4 D = vec4(0.0, 0.5, 1.0, 2.0);
  vec3 i  = floor(v + dot(v, C.yyy));
  vec3 x0 = v - i + dot(i, C.xxx);
  vec3 g = step(x0.yzx, x0.xyz);
  vec3 l = 1.0 - g;
  vec3 i1 = min( g.xyz, l.zxy );
  vec3 i2 = max( g.xyz, l.zxy );
  vec3 x1 = x0 - i1 + C.xxx;
  vec3 x2 = x0 - i2 + C.yyy;
  vec3 x3 = x0 - D.yyy;
  i = mod289(i);
  vec4 p = permute( permute( permute(
            i.z + vec4(0.0, i1.z, i2.z, 1.0))
          + i.y + vec4(0.0, i1.y, i2.y, 1.0))
          + i.x + vec4(0.0, i1.x, i2.x, 1.0));
  float n_ = 0.142857142857;
  vec3  ns = n_ * D.wyz - D.xzx;
  vec4 j = p - 49.0 * floor(p * ns.z * ns.z);
  vec4 x_ = floor(j * ns.z);
  vec4 y_ = floor(j - 7.0 * x_);
  vec4 x = x_ *ns.x + ns.yyyy;
  vec4 y = y_ *ns.x + ns.yyyy;
  vec4 h = 1.0 - abs(x) - abs(y);
  vec4 b0 = vec4( x.xy, y.xy );
  vec4 b1 = vec4( x.zw, y.zw );
  vec4 s0 = floor(b0)*2.0 + 1.0;
  vec4 s1 = floor(b1)*2.0 + 1.0;
  vec4 sh = -step(h, vec4(0.0));
  vec4 a0 = b0.xzyw + s0.xzyw*sh.xxyy ;
  vec4 a1 = b1.xzyw + s1.xzyw*sh.zzww ;
  vec3 p0 = vec3(a0.xy,h.x);
  vec3 p1 = vec3(a0.zw,h.y);
  vec3 p2 = vec3(a1.xy,h.z);
  vec3 p3 = vec3(a1.zw,h.w);
  vec4 norm = taylorInvSqrt(vec4(dot(p0,p0), dot(p1,p1), dot(p2,p2), dot(p3,p3)));
  p0 *= norm.x; p1 *= norm.y; p2 *= norm.z; p3 *= norm.w;
  vec4 m = max(0.6 - vec4(dot(x0,x0), dot(x1,x1), dot(x2,x2), dot(x3,x3)), 0.0);
  m = m * m;
  return 42.0 * dot( m*m, vec4( dot(p0,x0), dot(p1,x1), dot(p2,x2), dot(p3,x3) ) );
}
`

const VERT = /* glsl */ `
uniform float uTime;
uniform float uAmp;
varying vec3 vWorldNormal;
varying vec3 vWorldPos;
varying float vNoise;
${SNOISE}
void main(){
  float t = uTime * 0.28;
  float n = snoise(position * 1.35 + vec3(t)) * 0.6
          + snoise(position * 2.9 - vec3(t * 0.7)) * 0.2;
  vNoise = n;
  vec3 displaced = position + normal * n * uAmp;
  vec4 world = modelMatrix * vec4(displaced, 1.0);
  vWorldPos = world.xyz;
  vWorldNormal = normalize(mat3(modelMatrix) * normal);
  gl_Position = projectionMatrix * viewMatrix * world;
}
`

const FRAG = /* glsl */ `
precision highp float;
uniform vec3 uA; uniform vec3 uB; uniform vec3 uC; uniform vec3 uD;
varying vec3 vWorldNormal;
varying vec3 vWorldPos;
varying float vNoise;
void main(){
  float g = clamp(vWorldPos.y * 0.42 + 0.5 + vNoise * 0.42, 0.0, 1.0);
  vec3 col = mix(uA, uB, smoothstep(0.0, 0.42, g));
  col = mix(col, uC, smoothstep(0.4, 0.72, g));
  col = mix(col, uD, smoothstep(0.74, 1.0, g));
  vec3 viewDir = normalize(cameraPosition - vWorldPos);
  float fres = pow(1.0 - max(dot(viewDir, normalize(vWorldNormal)), 0.0), 2.6);
  col += fres * 0.55;
  col += smoothstep(0.55, 1.0, vNoise) * 0.18;
  gl_FragColor = vec4(col, 1.0);
}
`

function supportsWebGL(): boolean {
  try {
    const canvas = document.createElement('canvas')
    return !!(
      window.WebGLRenderingContext &&
      (canvas.getContext('webgl') || canvas.getContext('experimental-webgl'))
    )
  } catch {
    return false
  }
}

export function EnergyCore({ className }: { className?: string }) {
  const mountRef = useRef<HTMLDivElement>(null)
  const [fallback, setFallback] = useState(false)

  useEffect(() => {
    const mount = mountRef.current
    if (!mount) return

    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    if (reduce || !supportsWebGL()) {
      setFallback(true)
      return
    }

    let renderer: THREE.WebGLRenderer
    try {
      renderer = new THREE.WebGLRenderer({
        antialias: true,
        alpha: true,
        powerPreference: 'high-performance',
      })
    } catch {
      setFallback(true)
      return
    }

    let width = mount.clientWidth || 1
    let height = mount.clientHeight || 1
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
    renderer.setSize(width, height)
    renderer.outputColorSpace = THREE.SRGBColorSpace
    renderer.toneMapping = THREE.ACESFilmicToneMapping
    renderer.toneMappingExposure = 1.05
    mount.appendChild(renderer.domElement)
    renderer.domElement.style.width = '100%'
    renderer.domElement.style.height = '100%'
    renderer.domElement.style.display = 'block'

    const scene = new THREE.Scene()
    const camera = new THREE.PerspectiveCamera(38, width / height, 0.1, 100)
    camera.position.set(0, 0, 4.2)

    const uniforms = {
      uTime: { value: 0 },
      uAmp: { value: 0.42 },
      uA: { value: new THREE.Color('#7c3aed') },
      uB: { value: new THREE.Color('#c81e9e') },
      uC: { value: new THREE.Color('#f0452e') },
      uD: { value: new THREE.Color('#f6b43e') },
    }

    const geometry = new THREE.IcosahedronGeometry(1.18, 24)
    const material = new THREE.ShaderMaterial({
      uniforms,
      vertexShader: VERT,
      fragmentShader: FRAG,
    })
    const mesh = new THREE.Mesh(geometry, material)
    scene.add(mesh)

    const pointer = { x: 0, y: 0, tx: 0, ty: 0 }
    const onPointerMove = (e: PointerEvent) => {
      pointer.tx = (e.clientX / window.innerWidth - 0.5) * 2
      pointer.ty = (e.clientY / window.innerHeight - 0.5) * 2
    }
    window.addEventListener('pointermove', onPointerMove, { passive: true })

    const clock = new THREE.Clock()
    let visible = true
    let raf = 0

    const render = () => {
      raf = requestAnimationFrame(render)
      if (!visible) return
      uniforms.uTime.value = clock.getElapsedTime()
      pointer.x += (pointer.tx - pointer.x) * 0.05
      pointer.y += (pointer.ty - pointer.y) * 0.05
      mesh.rotation.y += 0.0016
      mesh.rotation.x = pointer.y * 0.28
      mesh.rotation.z = -pointer.x * 0.12
      camera.position.x = pointer.x * 0.35
      camera.position.y = -pointer.y * 0.25
      camera.lookAt(0, 0, 0)
      renderer.render(scene, camera)
    }
    render()

    const io = new IntersectionObserver(
      ([entry]) => {
        visible = entry.isIntersecting
      },
      { threshold: 0.01 }
    )
    io.observe(mount)

    const resize = () => {
      width = mount.clientWidth || 1
      height = mount.clientHeight || 1
      camera.aspect = width / height
      camera.updateProjectionMatrix()
      renderer.setSize(width, height)
    }
    const ro = new ResizeObserver(resize)
    ro.observe(mount)

    return () => {
      cancelAnimationFrame(raf)
      io.disconnect()
      ro.disconnect()
      window.removeEventListener('pointermove', onPointerMove)
      geometry.dispose()
      material.dispose()
      renderer.dispose()
      if (renderer.domElement.parentNode === mount) {
        mount.removeChild(renderer.domElement)
      }
    }
  }, [])

  if (fallback) {
    return (
      <div className={cn('pf-core-fallback', className)} aria-hidden='true' />
    )
  }
  return <div ref={mountRef} className={className} aria-hidden='true' />
}
