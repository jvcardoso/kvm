import React from 'react';
import { Drawer, DrawerProps } from 'antd';

import { useUiStore } from "@/hooks/stores";
import StatsSidebarHeader from "@components/StatsSidebarHeader";
import { dark_bg2_style } from "@/layout/theme_color";

export interface EnhancedSidebarDrawerProps extends Omit<DrawerProps, 'open' | 'placement' | 'children'> {
  title?: string;
  className?: string;
  targetView: string;

  placement: 'top' | 'bottom' | 'left' | 'right';
  drawerRender: () => React.ReactNode;
}

const EnhancedDrawer: React.FC<EnhancedSidebarDrawerProps> = ({
                                                              title='',
                                                              className = 'p-[20px]',
                                                              targetView,
                                                              placement,
                                                              drawerRender,
                                                              ...drawerProps
                                                             }) => {
  const sidebarView = useUiStore(state => state.sidebarView);
  const setSidebarView = useUiStore(state => state.setSidebarView);

  return (
    <Drawer
      open={sidebarView === targetView}
      placement={placement}
      width={"100%"}
      height={"100%"}
      getContainer={false}
      rootStyle={{ position: 'absolute'}}
      {...drawerProps}
      drawerRender={() => (
        <div className={`flex h-full min-h-0 flex-col ${dark_bg2_style} !pointer-events-auto`}>
          <div className={`${className} shrink-0`}>
            <StatsSidebarHeader title={title} setSidebarView={setSidebarView  } />
          </div>
          <div
            className={`${className} min-h-0 flex-1 space-y-4 overflow-y-auto overscroll-contain bg-white ${dark_bg2_style}`}>
            <div className={`min-h-full ${dark_bg2_style}`}>
              {drawerRender()}
            </div>
          </div>
        </div>
      )}
    />
  );
};

export default EnhancedDrawer;
