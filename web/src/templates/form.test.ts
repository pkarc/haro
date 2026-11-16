import { describe, expect, it } from 'vitest'
import type { TechTemplate } from '../core/types'
import { initialValues, isVisible, missingRequired, splitList, toParams } from './form'

const gitea: TechTemplate = {
  id: 'gitea', name: 'Gitea', description: 'd', kind: 'web',
  params: [
    { key: 'domain', label: 'Dominio', help: '', type: 'string', required: true, dependsOn: '', example: 'git.example.com' },
    { key: 'servers', label: 'Servidores', help: '', type: 'list', required: true, dependsOn: '', example: '10.0.0.40:3000' },
    { key: 'hsts', label: 'HSTS', help: '', type: 'bool', defaultValue: true, required: false, dependsOn: '', example: '' },
    { key: 'timeout', label: 'Timeout', help: '', type: 'string', defaultValue: '10m', required: false, dependsOn: '', example: '' },
    { key: 'ssh', label: 'SSH', help: '', type: 'bool', defaultValue: false, required: false, dependsOn: '', example: '' },
    { key: 'ssh_port', label: 'Puerto SSH', help: '', type: 'int', defaultValue: 22, required: false, dependsOn: 'ssh', example: '22' },
    { key: 'ssh_servers', label: 'Servidores SSH', help: '', type: 'list', required: true, dependsOn: 'ssh', example: '10.0.0.40:2222' },
  ],
}

describe('formulario de plantillas', () => {
  it('los valores iniciales salen de los valores por defecto, con el tipo del formulario', () => {
    expect(initialValues(gitea)).toEqual({ domain: '', servers: '', hsts: true, timeout: '10m', ssh: false, ssh_port: '22', ssh_servers: '' })
    const withList = { ...gitea, params: [{ ...gitea.params[1], defaultValue: ['a:1', 'b:2'] }] }
    expect(initialValues(withList).servers).toBe('a:1\nb:2')
  })

  it('un parámetro con dependsOn solo es visible con ese bool activado', () => {
    const ssh = gitea.params.find(p => p.key === 'ssh_port')!
    expect(isVisible(ssh, { ssh: false })).toBe(false)
    expect(isVisible(ssh, { ssh: true })).toBe(true)
    expect(isVisible(gitea.params[0], {})).toBe(true)
  })

  it('splitList admite líneas, comas y espacios sobrantes', () => {
    expect(splitList(' a:1\n\nb:2, c:3 ;d:4\n')).toEqual(['a:1', 'b:2', 'c:3', 'd:4'])
    expect(splitList('  \n ')).toEqual([])
  })

  it('detecta los obligatorios vacíos, ignorando los ocultos', () => {
    const v = initialValues(gitea)
    expect(missingRequired(gitea, v)).toEqual(['domain', 'servers'])
    expect(missingRequired(gitea, { ...v, domain: 'x.com', servers: ' \n' })).toEqual(['servers'])
    expect(missingRequired(gitea, { ...v, domain: 'x.com', servers: 'a:1' })).toEqual([])
    expect(missingRequired(gitea, { ...v, domain: 'x.com', servers: 'a:1', ssh: true })).toEqual(['ssh_servers'])
  })

  it('toParams envía valores tipados, omite vacíos y ocultos', () => {
    const v = { ...initialValues(gitea), domain: ' git.example.com ', servers: '10.0.0.40:3000\n10.0.0.41:3000' }
    expect(toParams(gitea, v)).toEqual({ domain: 'git.example.com', servers: ['10.0.0.40:3000', '10.0.0.41:3000'], hsts: true, timeout: '10m', ssh: false })
    const ssh = { ...v, ssh: true, ssh_port: '2222', ssh_servers: '10.0.0.40:2222', hsts: false, timeout: '' }
    expect(toParams(gitea, ssh)).toEqual({
      domain: 'git.example.com', servers: ['10.0.0.40:3000', '10.0.0.41:3000'], hsts: false, ssh: true, ssh_port: 2222, ssh_servers: ['10.0.0.40:2222'],
    })
  })

  it('un entero no numérico se envía como texto para que el núcleo lo rechace con su mensaje', () => {
    const v = { ...initialValues(gitea), domain: 'x.com', servers: 'a:1', ssh: true, ssh_port: 'abc', ssh_servers: 'a:22' }
    expect(toParams(gitea, v).ssh_port).toBe('abc')
  })
})
