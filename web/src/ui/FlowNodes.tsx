import { Fragment, type ReactNode } from 'react'

export type FlowNode = {
  label: ReactNode
  /** small rule-count cap inside the node, e.g. 「14,344 条」 */
  cap?: string
}

type FlowNodesProps = {
  /** 来源 nodes (终端/全部/排除) */
  source: FlowNode[]
  /** 目标 nodes (目标库 badges) */
  target: FlowNode[]
  /** 出口 node — highlighted accent; use 'err' for 阻断 endpoints in 访问控制 */
  exit: FlowNode
  exitTone?: 'accent' | 'err'
}

/** 「来源 → 目标 → 出口」pill flow (§9.2). */
export function FlowNodes({ source, target, exit, exitTone = 'accent' }: FlowNodesProps) {
  const renderNode = (node: FlowNode, key: string, extraClass = '') => (
    <span key={key} className={`flow-node ${extraClass}`.trim()}>
      {node.label}
      {node.cap ? <span className="flow-cap">{node.cap}</span> : null}
    </span>
  )
  const groups: Array<{ nodes: FlowNode[]; extraClass?: string }> = [
    { nodes: source },
    { nodes: target },
    { nodes: [exit], extraClass: exitTone === 'err' ? 'flow-node-err' : 'flow-node-hl' },
  ]
  return (
    <div className="flow-nodes">
      {groups.map((group, groupIndex) => (
        <Fragment key={groupIndex}>
          {groupIndex > 0 ? (
            <span className="flow-arrow" aria-hidden="true">
              →
            </span>
          ) : null}
          {group.nodes.map((node, nodeIndex) => renderNode(node, `${groupIndex}-${nodeIndex}`, group.extraClass ?? ''))}
        </Fragment>
      ))}
    </div>
  )
}

type FlowPillsProps = {
  nodes: FlowNode[]
  /** 'accent' = 出口高亮, 'err' = 阻断终点；默认中性胶囊 */
  tone?: 'accent' | 'err'
}

/** One group of flow pills without arrows — for a single table cell (§9.2). */
export function FlowPills({ nodes, tone }: FlowPillsProps) {
  const extraClass = tone === 'err' ? 'flow-node-err' : tone === 'accent' ? 'flow-node-hl' : ''
  return (
    <div className="flow-nodes">
      {nodes.map((node, index) => (
        <span key={index} className={`flow-node ${extraClass}`.trim()}>
          {node.label}
          {node.cap ? <span className="flow-cap">{node.cap}</span> : null}
        </span>
      ))}
    </div>
  )
}
