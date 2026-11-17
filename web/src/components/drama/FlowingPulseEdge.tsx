import React from "react";
import { BaseEdge, EdgeProps, getBezierPath } from "@xyflow/react";

export interface FlowingPulseEdgeData {
  color?: string;
  activePulse?: boolean;
  glow?: boolean;
}

export const FlowingPulseEdge: React.FC<EdgeProps> = ({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  style = {},
  markerEnd,
  selected,
  data,
}) => {
  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  const edgeData = (data as FlowingPulseEdgeData) || {};
  const strokeColor = edgeData.color || (selected ? "#F59E0B" : "#6366F1");
  const isActive = edgeData.activePulse || selected;

  return (
    <>
      {/* 1. 宽光晕底轨 (Glow Shadow) */}
      <path
        d={edgePath}
        fill="none"
        stroke={strokeColor}
        strokeWidth={selected ? 8 : 4}
        strokeOpacity={selected ? 0.35 : 0.15}
        strokeLinecap="round"
        className="transition-all duration-300"
      />

      {/* 2. 核心信号导轨 */}
      <BaseEdge
        id={id}
        path={edgePath}
        markerEnd={markerEnd}
        style={{
          ...style,
          stroke: strokeColor,
          strokeWidth: selected ? 2.5 : 1.8,
          strokeOpacity: selected ? 1 : 0.7,
        }}
      />

      {/* 3. 动态流光脉冲粒子 (ComfyUI Flow Animation) */}
      {isActive && (
        <path
          d={edgePath}
          fill="none"
          stroke={strokeColor}
          strokeWidth={selected ? 3 : 2.2}
          strokeDasharray="6 6"
          strokeLinecap="round"
          className="pointer-events-none"
          style={{
            animation: "flowingDash 1.2s linear infinite",
          }}
        />
      )}
    </>
  );
};

export default FlowingPulseEdge;
