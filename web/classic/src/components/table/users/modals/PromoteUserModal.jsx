/*
Copyright (C) 2025 QuantumNous

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

import React, { useState, useEffect } from 'react';
import { Modal, Form } from '@douyinfe/semi-ui';
import { API } from '../../../../helpers';

const ROLE_GROUP_ADMIN = 5;

const PromoteUserModal = ({ visible, onCancel, onConfirm, user, t }) => {
  const [groups, setGroups] = useState([]);
  const [selectedGroup, setSelectedGroup] = useState('');

  const isToGroupAdmin = user && user.role < ROLE_GROUP_ADMIN;
  const targetRoleName = isToGroupAdmin ? t('组内管理员') : t('管理员');

  useEffect(() => {
    if (visible && isToGroupAdmin) {
      API.get('/api/group/').then((res) => {
        const { success, data } = res.data;
        if (success) {
          setGroups(data || []);
          if (data && data.length > 0) {
            setSelectedGroup(user?.group || data[0]?.key || '');
          }
        }
      });
    }
  }, [visible, isToGroupAdmin, user]);

  const handleConfirm = () => {
    onConfirm(isToGroupAdmin ? selectedGroup : undefined);
  };

  return (
    <Modal
      title={t('确定要提升此用户吗？')}
      visible={visible}
      onCancel={onCancel}
      onOk={handleConfirm}
      type='warning'
      okButtonProps={{ disabled: isToGroupAdmin && !selectedGroup }}
    >
      <div style={{ marginBottom: 12 }}>
        {t('此操作将提升用户的权限级别')}：{targetRoleName}
      </div>
      {isToGroupAdmin && (
        <Form.Select
          label={t('所属分组')}
          style={{ width: '100%' }}
          value={selectedGroup}
          onChange={setSelectedGroup}
          optionList={groups.map((g) => ({
            label: g.key,
            value: g.key,
          }))}
          placeholder={t('请选择分组')}
        />
      )}
    </Modal>
  );
};

export default PromoteUserModal;
